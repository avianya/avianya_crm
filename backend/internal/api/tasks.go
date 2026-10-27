package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// Task handlers follow the same four steps as the lead handlers in leads.go.

// GET /api/tasks?status=open|completed  or  GET /api/tasks?from=&to=
//
// Every task across all leads, each with its lead attached. "open" (the
// default) is the working list, soonest due first; "completed" is the most
// recent history, latest first. A from/to date range instead returns every
// task due in it, whatever its status -- what the calendar shows.
func (h *Handler) listAllTasks(c *gin.Context) {
	if c.Query("from") != "" || c.Query("to") != "" {
		h.listTasksBetween(c)
		return
	}

	status := strings.ToLower(strings.TrimSpace(c.DefaultQuery("status", "open")))
	if status != "open" && status != "completed" {
		respondValidationError(c, model.ValidationErrors{
			"status": fmt.Sprintf(`%q is not one of "open", "completed"`, status),
		})
		return
	}

	items, err := h.store.ListAllTasks(c.Request.Context(), status == "completed")
	if err != nil {
		h.respondStoreError(c, err, "list all tasks")
		return
	}

	respondData(c, http.StatusOK, items)
}

// listTasksBetween handles GET /api/tasks?from=YYYY-MM-DD&to=YYYY-MM-DD.
func (h *Handler) listTasksBetween(c *gin.Context) {
	errs := model.ValidationErrors{}
	from, errFrom := time.Parse(model.DateLayout, c.Query("from"))
	to, errTo := time.Parse(model.DateLayout, c.Query("to"))
	if errFrom != nil {
		errs["from"] = "must be a date in YYYY-MM-DD format"
	}
	if errTo != nil {
		errs["to"] = "must be a date in YYYY-MM-DD format"
	}
	if !errs.Any() {
		switch days := int(to.Sub(from).Hours() / 24); {
		case days < 0:
			errs["to"] = "must not be before from"
		case days >= model.MaxRangeDays:
			errs["to"] = fmt.Sprintf("the range can be at most %d days", model.MaxRangeDays)
		}
	}
	if errs.Any() {
		respondValidationError(c, errs)
		return
	}

	items, err := h.store.ListTasksBetween(c.Request.Context(),
		from.Format(model.DateLayout), to.Format(model.DateLayout))
	if err != nil {
		h.respondStoreError(c, err, "list tasks in range")
		return
	}
	respondData(c, http.StatusOK, items)
}

// GET /api/leads/:id/tasks
func (h *Handler) listTasks(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	tasks, err := h.store.ListTasks(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "list tasks")
		return
	}

	respondData(c, http.StatusOK, tasks)
}

// POST /api/leads/:id/tasks
func (h *Handler) createTask(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var in model.TaskInput
	if !bindJSON(c, &in) {
		return
	}

	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	task, err := h.store.CreateTask(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "create task")
		return
	}

	respondData(c, http.StatusCreated, task)
}

// PATCH /api/tasks/:id
//
// {"completed": bool} ticks a task off or reopens it; {"cancelled": bool}
// cancels an event or restores it. Responds with the task and, when this call
// completed or cancelled it, the timeline entry that was written -- so the
// detail page can add it to the timeline without a second request.
func (h *Handler) updateTaskStatus(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var in model.TaskStatusInput
	if !bindJSON(c, &in) {
		return
	}

	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	var (
		task     model.Task
		activity *model.Activity
		err      error
	)
	if in.Cancelled != nil {
		task, activity, err = h.store.SetTaskCancelled(c.Request.Context(), id, *in.Cancelled)
	} else {
		task, activity, err = h.store.SetTaskCompleted(c.Request.Context(), id, *in.Completed)
	}
	if err == nil {
		// A meeting marked done is evidence for the lead's quality.
		h.refreshQuality(c, task.LeadID)
	}
	if err != nil {
		h.respondStoreError(c, err, "update task")
		return
	}

	respondData(c, http.StatusOK, gin.H{
		"task":     task,
		"activity": activity, // null unless this call completed or cancelled the task
	})
}

// PUT /api/tasks/:id
//
// The edit form: replaces the title, date and time slot in one go. The body is
// the same as for creating a task.
func (h *Handler) updateTask(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var in model.TaskInput
	if !bindJSON(c, &in) {
		return
	}

	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	task, err := h.store.UpdateTask(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "update task")
		return
	}

	respondData(c, http.StatusOK, task)
}

// PUT /api/tasks/:id/schedule
//
// Moves a task on the calendar. The body replaces the whole slot: date, start
// time and duration, with no start time meaning all-day.
func (h *Handler) rescheduleTask(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var in model.TaskScheduleInput
	if !bindJSON(c, &in) {
		return
	}

	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	task, err := h.store.RescheduleTask(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "reschedule task")
		return
	}

	respondData(c, http.StatusOK, task)
}

// DELETE /api/tasks/:id
func (h *Handler) deleteTask(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	if err := h.store.DeleteTask(c.Request.Context(), id); err != nil {
		h.respondStoreError(c, err, "delete task")
		return
	}

	c.Status(http.StatusNoContent)
}
