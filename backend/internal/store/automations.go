package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// The run count and last run come from automation_runs, so they can never
// drift from what the rule actually did.
const automationColumns = `
	r.id, r.name, r.enabled, r.trigger_event, r.sources, r.min_deal_value,
	r.task_title, r.task_type, r.due_in_days, r.assignee_id,
	(SELECT count(*) FROM automation_runs ar WHERE ar.rule_id = r.id),
	(SELECT max(ar.ran_at) FROM automation_runs ar WHERE ar.rule_id = r.id),
	r.created_at, r.updated_at`

func scanAutomationRule(row rowScanner) (model.AutomationRule, error) {
	var (
		rule              model.AutomationRule
		trigger, taskType string
		sources           []string
	)
	err := row.Scan(&rule.ID, &rule.Name, &rule.Enabled, &trigger, &sources, &rule.MinDealValue,
		&rule.TaskTitle, &taskType, &rule.DueInDays, &rule.AssigneeID,
		&rule.RunCount, &rule.LastRunAt, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return model.AutomationRule{}, err
	}
	rule.Trigger = model.AutomationTrigger(trigger)
	rule.TaskType = model.TaskType(taskType)
	rule.Sources = make([]model.Source, len(sources))
	for i, s := range sources {
		rule.Sources[i] = model.Source(s)
	}
	return rule, nil
}

func sourceStrings(sources []model.Source) []string {
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = string(s)
	}
	return out
}

func duplicateRuleName() error {
	return &FieldError{Field: "name", Message: "is already used by another rule"}
}

// ruleWriteError translates the constraint errors a rule insert or update can
// hit.
func ruleWriteError(err error, what string) error {
	if isUniqueViolation(err, "automation_rules_name_lower_key") {
		return duplicateRuleName()
	}
	if isForeignKeyViolationOn(err, "automation_rules_assignee_id_fkey") {
		return unknownMember("assignee_id")
	}
	return fmt.Errorf("%s: %w", what, err)
}

// ListAutomationRules returns every rule, oldest first: the order they run in.
// Never nil.
func (s *Store) ListAutomationRules(ctx context.Context) ([]model.AutomationRule, error) {
	rows, err := s.query(ctx,
		fmt.Sprintf(`SELECT %s FROM automation_rules r ORDER BY r.id`, automationColumns))
	if err != nil {
		return nil, fmt.Errorf("list automation rules: %w", err)
	}
	rules, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.AutomationRule, error) {
		return scanAutomationRule(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan automation rules: %w", err)
	}
	if rules == nil {
		rules = []model.AutomationRule{}
	}
	return rules, nil
}

// getAutomationRule reads one rule back after a write, run counts and all.
func (s *Store) getAutomationRule(ctx context.Context, id int64) (model.AutomationRule, error) {
	rule, err := scanAutomationRule(s.queryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM automation_rules r WHERE r.id = $1`, automationColumns), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.AutomationRule{}, ErrNotFound
		}
		return model.AutomationRule{}, fmt.Errorf("get automation rule %d: %w", id, err)
	}
	return rule, nil
}

// CreateAutomationRule adds a rule. It applies to leads created from now on.
func (s *Store) CreateAutomationRule(ctx context.Context, in model.AutomationRuleInput) (model.AutomationRule, error) {
	var id int64
	err := s.queryRow(ctx, `
		INSERT INTO automation_rules (name, enabled, trigger_event, sources, min_deal_value,
		                              task_title, task_type, due_in_days, assignee_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		in.Name, *in.Enabled, string(in.Trigger), sourceStrings(in.Sources), in.MinDealValue,
		in.TaskTitle, string(in.TaskType), in.DueInDays, in.AssigneeID,
	).Scan(&id)
	if err != nil {
		return model.AutomationRule{}, ruleWriteError(err, "insert automation rule")
	}
	return s.getAutomationRule(ctx, id)
}

// UpdateAutomationRule replaces a rule's fields. Tasks it already planned are
// left as they are.
func (s *Store) UpdateAutomationRule(ctx context.Context, id int64, in model.AutomationRuleInput) (model.AutomationRule, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE automation_rules
		SET name = $2, enabled = $3, trigger_event = $4, sources = $5, min_deal_value = $6,
		    task_title = $7, task_type = $8, due_in_days = $9, assignee_id = $10,
		    updated_at = now()
		WHERE id = $1`,
		id, in.Name, *in.Enabled, string(in.Trigger), sourceStrings(in.Sources), in.MinDealValue,
		in.TaskTitle, string(in.TaskType), in.DueInDays, in.AssigneeID)
	if err != nil {
		return model.AutomationRule{}, ruleWriteError(err, fmt.Sprintf("update automation rule %d", id))
	}
	if tag.RowsAffected() == 0 {
		return model.AutomationRule{}, ErrNotFound
	}
	return s.getAutomationRule(ctx, id)
}

// DeleteAutomationRule removes a rule and its run history. Tasks it planned,
// and their timeline entries, stay.
func (s *Store) DeleteAutomationRule(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM automation_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete automation rule %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// runAutomationsTx runs the enabled rules for trigger against a lead, inside
// the transaction that caused it. Each matching rule plans one task, writes a
// timeline entry saying so, and records the run; a rule that already ran for
// the lead is skipped.
//
// The task goes to the rule's assignee, else the lead's owner, and gets that
// person's default reminder, the same as an event planned by hand.
func runAutomationsTx(ctx context.Context, tx pgx.Tx, trigger model.AutomationTrigger, lead model.Lead) error {
	rows, err := tx.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM automation_rules r
		WHERE r.enabled AND r.trigger_event = $1
		  AND NOT EXISTS (SELECT 1 FROM automation_runs ar WHERE ar.rule_id = r.id AND ar.lead_id = $2)
		ORDER BY r.id`, automationColumns), string(trigger), lead.ID)
	if err != nil {
		return fmt.Errorf("load automation rules: %w", err)
	}
	rules, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.AutomationRule, error) {
		return scanAutomationRule(row)
	})
	if err != nil {
		return fmt.Errorf("scan automation rules: %w", err)
	}

	var matching []model.AutomationRule
	for _, rule := range rules {
		if rule.Matches(lead) {
			matching = append(matching, rule)
		}
	}
	if len(matching) == 0 {
		return nil
	}

	ownerName := ""
	if lead.OwnerID != nil {
		if err := tx.QueryRow(ctx,
			`SELECT name FROM team_members WHERE id = $1`, *lead.OwnerID,
		).Scan(&ownerName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("owner name for automations: %w", err)
		}
	}

	for _, rule := range matching {
		title := rule.TaskTitleFor(lead, ownerName)

		var taskID int64
		if err := tx.QueryRow(ctx, `
			WITH who AS (SELECT COALESCE($5::bigint, $6::bigint) AS id)
			INSERT INTO tasks (lead_id, title, type, due_date, assignee_id, reminder_minutes)
			SELECT $1, $2, $3, CURRENT_DATE + $4::int, who.id,
			       (SELECT default_reminder_minutes FROM team_members WHERE id = who.id)
			FROM who
			RETURNING id`,
			lead.ID, title, string(rule.TaskType), rule.DueInDays, rule.AssigneeID, lead.OwnerID,
		).Scan(&taskID); err != nil {
			return fmt.Errorf("automation rule %d task: %w", rule.ID, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO automation_runs (rule_id, lead_id, task_id) VALUES ($1, $2, $3)`,
			rule.ID, lead.ID, taskID); err != nil {
			return fmt.Errorf("record automation rule %d run: %w", rule.ID, err)
		}

		note := model.AutomationNote(rule.Name, rule.TaskType, title, rule.DueInDays)
		if err := insertActivityTx(ctx, tx, lead.ID, model.ActivityAutomation, note); err != nil {
			return err
		}
	}
	return nil
}
