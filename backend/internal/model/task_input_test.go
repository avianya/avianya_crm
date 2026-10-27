package model

import "testing"

func TestTaskInputTimeSlot(t *testing.T) {
	intp := func(n int) *int { return &n }

	cases := []struct {
		name    string
		in      TaskInput
		badKeys []string
	}{
		{"all day", TaskInput{Title: "Call", DueDate: "2026-10-10"}, nil},
		{"timed", TaskInput{Title: "Call", DueDate: "2026-10-10", StartTime: "09:30", DurationMinutes: intp(45)}, nil},
		{"bad time", TaskInput{Title: "Call", DueDate: "2026-10-10", StartTime: "25:00"}, []string{"start_time"}},
		{"duration without time", TaskInput{Title: "Call", DueDate: "2026-10-10", DurationMinutes: intp(30)}, []string{"duration_minutes"}},
		{"duration too short", TaskInput{Title: "Call", DueDate: "2026-10-10", StartTime: "09:00", DurationMinutes: intp(1)}, []string{"duration_minutes"}},
		{"duration too long", TaskInput{Title: "Call", DueDate: "2026-10-10", StartTime: "09:00", DurationMinutes: intp(1441)}, []string{"duration_minutes"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Normalize()
			errs := tc.in.Validate()
			if len(errs) != len(tc.badKeys) {
				t.Fatalf("errors = %v, want keys %v", errs, tc.badKeys)
			}
			for _, key := range tc.badKeys {
				if _, ok := errs[key]; !ok {
					t.Errorf("missing error for %q in %v", key, errs)
				}
			}
		})
	}
}

func TestTaskInputDefaultsDurationWhenTimed(t *testing.T) {
	in := TaskInput{Title: "Call", DueDate: "2026-10-10", StartTime: " 14:00 "}
	in.Normalize()
	if in.StartTime != "14:00" || in.DurationMinutes == nil || *in.DurationMinutes != DefaultTaskDuration {
		t.Fatalf("got start %q duration %v", in.StartTime, in.DurationMinutes)
	}
}

func TestTaskScheduleInput(t *testing.T) {
	allDay := TaskScheduleInput{DueDate: "2026-10-12"}
	allDay.Normalize()
	if errs := allDay.Validate(); errs.Any() || allDay.DurationMinutes != nil {
		t.Fatalf("all-day: errs %v, duration %v", errs, allDay.DurationMinutes)
	}

	timed := TaskScheduleInput{DueDate: "2026-10-12", StartTime: "16:15"}
	timed.Normalize()
	if errs := timed.Validate(); errs.Any() || *timed.DurationMinutes != DefaultTaskDuration {
		t.Fatalf("timed: errs %v", errs)
	}

	bad := TaskScheduleInput{DueDate: "12/10/2026", StartTime: "4pm"}
	bad.Normalize()
	errs := bad.Validate()
	if _, ok := errs["due_date"]; !ok {
		t.Errorf("want due_date error, got %v", errs)
	}
	if _, ok := errs["start_time"]; !ok {
		t.Errorf("want start_time error, got %v", errs)
	}
}

func TestTaskTypeDefaultsAndValidation(t *testing.T) {
	in := TaskInput{Title: "Call", DueDate: "2026-10-10"}
	in.Normalize()
	if in.Type != TaskTodo {
		t.Fatalf("default type = %q, want todo", in.Type)
	}

	in.Type = " CALL "
	in.Normalize()
	if errs := in.Validate(); errs.Any() || in.Type != TaskCall {
		t.Fatalf("type %q, errs %v", in.Type, errs)
	}

	in.Type = "fax"
	if _, ok := in.Validate()["type"]; !ok {
		t.Fatal("want an error for an unknown type")
	}
}

func TestCompletionActivity(t *testing.T) {
	cases := map[TaskType]ActivityType{
		TaskTodo:    ActivityTaskCompleted,
		TaskCall:    ActivityCall,
		TaskEmail:   ActivityEmail,
		TaskMeeting: ActivityMeeting,
	}
	for taskType, want := range cases {
		if got := taskType.CompletionActivity(); got != want {
			t.Errorf("%s -> %s, want %s", taskType, got, want)
		}
	}
}
