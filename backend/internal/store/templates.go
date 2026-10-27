package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

const templateColumns = `id, name, subject, body, created_at, updated_at`

func scanTemplate(row rowScanner) (model.EmailTemplate, error) {
	var t model.EmailTemplate
	err := row.Scan(&t.ID, &t.Name, &t.Subject, &t.Body, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// duplicateTemplateName is the error for a name another template already has.
func duplicateTemplateName() error {
	return &FieldError{Field: "name", Message: "is already used by another template"}
}

// ListEmailTemplates returns every template, by name. Never nil.
func (s *Store) ListEmailTemplates(ctx context.Context) ([]model.EmailTemplate, error) {
	rows, err := s.query(ctx,
		fmt.Sprintf(`SELECT %s FROM email_templates ORDER BY lower(name), id`, templateColumns))
	if err != nil {
		return nil, fmt.Errorf("list email templates: %w", err)
	}
	templates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.EmailTemplate, error) {
		return scanTemplate(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan email templates: %w", err)
	}
	if templates == nil {
		templates = []model.EmailTemplate{}
	}
	return templates, nil
}

// CreateEmailTemplate adds a template. A name already in use, ignoring case,
// is a FieldError on "name".
func (s *Store) CreateEmailTemplate(ctx context.Context, in model.EmailTemplateInput) (model.EmailTemplate, error) {
	sql := fmt.Sprintf(`
		INSERT INTO email_templates (name, subject, body)
		VALUES ($1, $2, $3)
		RETURNING %s`, templateColumns)

	t, err := scanTemplate(s.queryRow(ctx, sql, in.Name, in.Subject, in.Body))
	if err != nil {
		if isUniqueViolation(err, "email_templates_name_lower_key") {
			return model.EmailTemplate{}, duplicateTemplateName()
		}
		return model.EmailTemplate{}, fmt.Errorf("insert email template: %w", err)
	}
	return t, nil
}

// UpdateEmailTemplate replaces a template's fields.
func (s *Store) UpdateEmailTemplate(ctx context.Context, id int64, in model.EmailTemplateInput) (model.EmailTemplate, error) {
	sql := fmt.Sprintf(`
		UPDATE email_templates
		SET name = $2, subject = $3, body = $4, updated_at = now()
		WHERE id = $1
		RETURNING %s`, templateColumns)

	t, err := scanTemplate(s.queryRow(ctx, sql, id, in.Name, in.Subject, in.Body))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.EmailTemplate{}, ErrNotFound
		}
		if isUniqueViolation(err, "email_templates_name_lower_key") {
			return model.EmailTemplate{}, duplicateTemplateName()
		}
		return model.EmailTemplate{}, fmt.Errorf("update email template %d: %w", id, err)
	}
	return t, nil
}

// DeleteEmailTemplate removes a template. Emails already sent from it stay on
// their timelines: they were copied there, not linked.
func (s *Store) DeleteEmailTemplate(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM email_templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete email template %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LeadForEmail returns a lead with its owner's name and email, as needed to
// fill in and send an email to it. The owner fields are "" for an unowned
// lead. ErrNotFound for a missing lead.
func (s *Store) LeadForEmail(ctx context.Context, leadID int64) (lead model.Lead, ownerName, ownerEmail string, err error) {
	sql := fmt.Sprintf(`
		SELECT %s, COALESCE(m.name, ''), COALESCE(m.email, '')
		FROM leads l
		LEFT JOIN team_members m ON m.id = l.owner_id
		WHERE l.id = $1`, leadColumnsQualified)

	lead, err = scanLead(s.queryRow(ctx, sql, leadID), &ownerName, &ownerEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Lead{}, "", "", ErrNotFound
		}
		return model.Lead{}, "", "", fmt.Errorf("lead %d for email: %w", leadID, err)
	}
	return lead, ownerName, ownerEmail, nil
}
