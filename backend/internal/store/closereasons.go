package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// Close reasons are stored on the lead (close_reason, close_note) while it is
// Won or Lost. These helpers are shared by the three ways a lead's stage is
// set: creating it, editing it, and the inline stage change.

// closeReasonSet is the SET clause for close_reason and close_note in an
// UPDATE of leads l joined to its pre-update row prev, with the new stage at
// $stage, the reason and note at $reason and $note, and whether either was
// sent at $given:
//
//   - an open stage clears both;
//   - a reason sent with a closed stage replaces both;
//   - none sent keeps them while the lead stays in the same closed stage (an
//     edit of an old lost lead's phone number), and clears them when it is
//     moving into it (a win with the reason skipped).
func closeReasonSet(stage, reason, note, given int) string {
	return fmt.Sprintf(`
		close_reason = CASE
			WHEN $%[1]d::lead_stage NOT IN ('won', 'lost') THEN NULL
			WHEN $%[4]d THEN $%[2]d
			WHEN prev.stage = $%[1]d::lead_stage THEN prev.close_reason
		END,
		close_note = CASE
			WHEN $%[1]d::lead_stage NOT IN ('won', 'lost') THEN NULL
			WHEN $%[4]d THEN $%[3]d
			WHEN prev.stage = $%[1]d::lead_stage THEN prev.close_note
		END`, stage, reason, note, given)
}

// requireLostReason is the one rule a stage change can break: a lead moving
// into Lost must say why. entering is false when it was Lost already.
func requireLostReason(stage model.Stage, entering bool, reason *model.CloseReason) error {
	if stage == model.StageLost && entering && reason == nil {
		return &FieldError{Field: "close_reason", Message: "is required when a deal is lost: say why, so the loss report can learn from it"}
	}
	return nil
}

// reasonSuffix is the " Reason: ..." added to a stage change's timeline
// entry, or "" when there is no reason.
func reasonSuffix(stage model.Stage, reason *model.CloseReason, note *string) string {
	if text := model.ReasonText(stage, reason, note); text != "" {
		return " Reason: " + text + "."
	}
	return ""
}

// recordStageOrReasonTx writes the timeline (and stage history) for a lead
// whose stage, or close reason, may just have changed. from is the stage it
// had, prevReason and prevNote the reason it had; lead is the row as updated.
func recordStageOrReasonTx(ctx context.Context, tx pgx.Tx, lead model.Lead, from model.Stage,
	prevReason *model.CloseReason, prevNote *string) error {

	if from != lead.Stage {
		return insertStageChangeTx(ctx, tx, lead.ID, from, lead.Stage,
			reasonSuffix(lead.Stage, lead.CloseReason, lead.CloseNote))
	}
	if !lead.Stage.IsClosed() ||
		(equalPtr(prevReason, lead.CloseReason) && equalPtr(prevNote, lead.CloseNote)) {
		return nil
	}

	// Same closed stage, new reason: say so, so the timeline keeps the old one.
	text := model.ReasonText(lead.Stage, lead.CloseReason, lead.CloseNote)
	if text == "" {
		text = "none given"
	}
	note := fmt.Sprintf("%s reason changed to: %s.", lead.Stage.Label(), text)
	return insertActivityTx(ctx, tx, lead.ID, model.ActivityStageChange, note)
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
