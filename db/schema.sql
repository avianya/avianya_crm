-- ============================================================================
-- CRM Lead Management — PostgreSQL schema
--
-- Idempotent: safe to run more than once against the same database.
-- Run with:  psql "$DATABASE_URL" -f db/schema.sql
-- ============================================================================

BEGIN;

-- ---------------------------------------------------------------------------
-- Enum types
--
-- Enums are used instead of free-text columns so the database itself rejects
-- invalid values -- the API is not the only line of defence.
--
-- IMPORTANT: a Postgres enum sorts by the order its labels were DECLARED, not
-- alphabetically. Declaring lead_stage in lifecycle order therefore gives us
-- "ORDER BY stage" = "order by how far along the pipeline the lead is" for
-- free, with no extra sort_order column to keep in sync.
-- ---------------------------------------------------------------------------

DO $$
BEGIN
    CREATE TYPE lead_stage AS ENUM (
        'new',
        'contacted',
        'qualified',
        'proposal_sent',
        'negotiation',
        'won',
        'lost'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    CREATE TYPE lead_source AS ENUM (
        'website',
        'referral',
        'linkedin',
        'cold_call',
        'other'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- 'created' and 'stage_change' rows are written by the backend automatically so
-- the timeline tells the whole story of the lead. The rest are user-logged.
DO $$
BEGIN
    CREATE TYPE activity_type AS ENUM (
        'created',
        'stage_change',
        'note',
        'call',
        'email',
        'meeting'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- ---------------------------------------------------------------------------
-- leads
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS leads (
    id         BIGSERIAL     PRIMARY KEY,

    name       TEXT          NOT NULL,
    company    TEXT          NOT NULL,
    email      TEXT          NOT NULL,
    phone      TEXT,                                   -- optional
    source     lead_source   NOT NULL DEFAULT 'other',
    stage      lead_stage    NOT NULL DEFAULT 'new',
    notes      TEXT          NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT now(),

    -- Data-integrity constraints. These mirror the validation in the Go layer;
    -- duplicated on purpose, because a bad migration or a manual psql INSERT
    -- should not be able to create a nameless lead.
    CONSTRAINT leads_name_not_blank
        CHECK (btrim(name) <> '' AND char_length(name) <= 120),
    CONSTRAINT leads_company_not_blank
        CHECK (btrim(company) <> '' AND char_length(company) <= 120),
    CONSTRAINT leads_email_shape
        CHECK (email ~* '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
               AND char_length(email) <= 254),
    CONSTRAINT leads_phone_shape
        CHECK (phone IS NULL OR phone ~ '^[0-9+()./[:space:]-]{6,24}$'),
    CONSTRAINT leads_notes_length
        CHECK (char_length(notes) <= 5000)
);

-- One lead per email address, case-insensitively: "Ada@x.com" and "ada@x.com"
-- are the same person. A functional unique index is how you express that in
-- Postgres. The API turns the resulting 23505 error into HTTP 409 Conflict.
CREATE UNIQUE INDEX IF NOT EXISTS leads_email_lower_key
    ON leads (lower(email));

-- The leads list is almost always "filter by stage, newest first", so a
-- composite index in exactly that shape lets one index serve both the WHERE
-- and the ORDER BY.
CREATE INDEX IF NOT EXISTS leads_stage_created_at_idx
    ON leads (stage, created_at DESC);

-- Unfiltered list + the dashboard's "5 most recent leads".
CREATE INDEX IF NOT EXISTS leads_created_at_idx
    ON leads (created_at DESC);

-- "Where are our leads coming from" filtering/grouping.
CREATE INDEX IF NOT EXISTS leads_source_idx
    ON leads (source);

-- ---------------------------------------------------------------------------
-- activities  (the timeline / audit trail for a lead)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS activities (
    id         BIGSERIAL     PRIMARY KEY,

    -- ON DELETE CASCADE: deleting a lead must not leave orphan timeline rows.
    -- The database enforces this, so the Go code never has to remember to.
    lead_id    BIGINT        NOT NULL
                             REFERENCES leads (id) ON DELETE CASCADE,

    type       activity_type NOT NULL DEFAULT 'note',
    note       TEXT          NOT NULL,
    created_at TIMESTAMPTZ   NOT NULL DEFAULT now(),

    CONSTRAINT activities_note_not_blank
        CHECK (btrim(note) <> '' AND char_length(note) <= 2000)
);

-- Every read of this table is "the timeline of ONE lead, newest first".
-- Note there is no standalone index on lead_id: this composite one already
-- covers lead_id lookups (leftmost-prefix rule), so a second index would only
-- cost write throughput.
CREATE INDEX IF NOT EXISTS activities_lead_id_created_at_idx
    ON activities (lead_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- updated_at maintenance
--
-- Done with a trigger rather than in application code: any writer -- the Go
-- API, a psql session, a future admin script -- gets a correct updated_at.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS leads_set_updated_at ON leads;
CREATE TRIGGER leads_set_updated_at
    BEFORE UPDATE ON leads
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

COMMIT;

-- ---------------------------------------------------------------------------
-- Deal value
--
-- Whole rupees in a BIGINT rather than NUMERIC: a CRM forecast never needs
-- paise, and an integer travels through Go and JSON without any rounding
-- surprises. NULL means "not estimated yet", which is different from 0.
--
-- ADD COLUMN IF NOT EXISTS keeps this file idempotent for databases created
-- before the column existed.
-- ---------------------------------------------------------------------------

ALTER TABLE leads ADD COLUMN IF NOT EXISTS deal_value BIGINT;

DO $$
BEGIN
    ALTER TABLE leads ADD CONSTRAINT leads_deal_value_range
        CHECK (deal_value IS NULL OR (deal_value >= 0 AND deal_value <= 1000000000000));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- ---------------------------------------------------------------------------
-- tasks  (follow-ups that belong to a lead)
-- ---------------------------------------------------------------------------

-- Completing a task writes a timeline entry, so the activity log records the
-- follow-up actually happening. ADD VALUE IF NOT EXISTS is idempotent; it is
-- outside the transaction above because a new enum label cannot be used in the
-- same transaction that adds it.
ALTER TYPE activity_type ADD VALUE IF NOT EXISTS 'task_completed';

CREATE TABLE IF NOT EXISTS tasks (
    id           BIGSERIAL    PRIMARY KEY,

    lead_id      BIGINT       NOT NULL
                              REFERENCES leads (id) ON DELETE CASCADE,

    title        TEXT         NOT NULL,
    due_date     DATE         NOT NULL,

    -- NULL while open. A timestamp rather than a boolean, so "when was this
    -- done" is never lost.
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT tasks_title_not_blank
        CHECK (btrim(title) <> '' AND char_length(title) <= 200)
);

-- Optional time slot, like a meeting on a calendar. Both NULL means an all-day
-- task. start_time is a wall-clock TIME with no zone, for the same reason
-- due_date is a DATE: "10:00 on the 10th" means 10:00 wherever you are.
-- Added with ALTER so an existing database picks them up on a re-run.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS start_time       TIME;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS duration_minutes INTEGER;

DO $$
BEGIN
    ALTER TABLE tasks ADD CONSTRAINT tasks_time_slot_valid
        CHECK (
            (start_time IS NULL AND duration_minutes IS NULL)
            OR (start_time IS NOT NULL AND duration_minutes BETWEEN 5 AND 1440)
        );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- What kind of follow-up a task is. Completing a call, email, meeting or demo
-- logs it on the timeline as that kind of contact; a to-do or follow-up logs
-- task_completed. Plain TEXT with a CHECK rather than an enum, so adding a type
-- is a constraint change instead of an enum migration -- dropped and re-added
-- here so an older database picks up the newer list.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'todo';

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_type_valid;
ALTER TABLE tasks ADD CONSTRAINT tasks_type_valid
    CHECK (type IN ('todo', 'call', 'email', 'meeting', 'demo', 'follow_up'));

-- A lead's task list: open first, then by due date.
CREATE INDEX IF NOT EXISTS tasks_lead_id_due_date_idx
    ON tasks (lead_id, due_date);

-- The dashboard's "due today / overdue" panel only ever looks at open tasks,
-- so a partial index keeps completed history out of it entirely.
CREATE INDEX IF NOT EXISTS tasks_open_due_date_idx
    ON tasks (due_date)
    WHERE completed_at IS NULL;

-- ---------------------------------------------------------------------------
-- Team members  (who owns a lead, who an event is assigned to)
--
-- There is no login: this is a list of people reminders can be sent to, not a
-- user account table. Each member chooses whether reminders reach them by email
-- and which reminder new events get by default.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS team_members (
    id                       BIGSERIAL    PRIMARY KEY,
    name                     TEXT         NOT NULL,
    email                    TEXT         NOT NULL,
    email_reminders          BOOLEAN      NOT NULL DEFAULT true,
    -- NULL means new events start with no reminder.
    default_reminder_minutes INTEGER,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT team_members_name_not_blank
        CHECK (btrim(name) <> '' AND char_length(name) <= 120),
    CONSTRAINT team_members_email_format
        CHECK (email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$' AND char_length(email) <= 254),
    CONSTRAINT team_members_default_reminder_valid
        CHECK (default_reminder_minutes IS NULL OR default_reminder_minutes IN (5, 15, 30, 1440))
);

CREATE UNIQUE INDEX IF NOT EXISTS team_members_email_lower_key
    ON team_members (lower(email));

-- The lead's owner. Deleting a member leaves their leads unowned, not deleted.
ALTER TABLE leads ADD COLUMN IF NOT EXISTS owner_id BIGINT
    REFERENCES team_members (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Events: tasks with notes, an assignee, a reminder and a cancelled state
-- ---------------------------------------------------------------------------

-- Cancelling an event is recorded on the timeline. Outside any transaction for
-- the same reason as task_completed above.
ALTER TYPE activity_type ADD VALUE IF NOT EXISTS 'task_cancelled';

ALTER TABLE tasks ADD COLUMN IF NOT EXISTS notes            TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS assignee_id      BIGINT
    REFERENCES team_members (id) ON DELETE SET NULL;
-- Minutes before the start to remind; NULL means no reminder.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS reminder_minutes INTEGER;
-- When the reminder is due, kept up to date by the trigger below.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS remind_at        TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS cancelled_at     TIMESTAMPTZ;

DO $$
BEGIN
    ALTER TABLE tasks ADD CONSTRAINT tasks_notes_length
        CHECK (char_length(notes) <= 2000);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE tasks ADD CONSTRAINT tasks_reminder_valid
        CHECK (reminder_minutes IS NULL OR reminder_minutes IN (5, 15, 30, 1440));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    -- An event is completed or cancelled, never both.
    ALTER TABLE tasks ADD CONSTRAINT tasks_completed_or_cancelled
        CHECK (completed_at IS NULL OR cancelled_at IS NULL);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- remind_at is derived from the date, start time and reminder, and cleared once
-- the event is completed or cancelled. A trigger rather than application code,
-- so every write -- create, edit, drag on the calendar, complete, cancel --
-- keeps it right without each query having to remember.
--
-- The start is read in the session's time zone (the API sets it from
-- APP_TIMEZONE). An all-day event is treated as starting at 09:00.
CREATE OR REPLACE FUNCTION tasks_set_remind_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.reminder_minutes IS NULL
       OR NEW.completed_at IS NOT NULL
       OR NEW.cancelled_at IS NOT NULL THEN
        NEW.remind_at := NULL;
    ELSE
        NEW.remind_at := (NEW.due_date + COALESCE(NEW.start_time, TIME '09:00'))::timestamptz
                         - make_interval(mins => NEW.reminder_minutes);
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS tasks_set_remind_at ON tasks;
CREATE TRIGGER tasks_set_remind_at
    BEFORE INSERT OR UPDATE ON tasks
    FOR EACH ROW
    EXECUTE FUNCTION tasks_set_remind_at();

-- The reminder job looks for reminders that have come due.
CREATE INDEX IF NOT EXISTS tasks_remind_at_idx
    ON tasks (remind_at)
    WHERE remind_at IS NOT NULL;

-- ---------------------------------------------------------------------------
-- notifications  (one per reminder that has come due)
--
-- The row is both the in-app notification and the email's delivery record.
-- UNIQUE (task_id, remind_at) is what stops a reminder being raised twice --
-- even with two API servers running -- while a rescheduled event, which has a
-- new remind_at, gets a reminder of its own.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS notifications (
    id                    BIGSERIAL    PRIMARY KEY,
    task_id               BIGINT       NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    remind_at             TIMESTAMPTZ  NOT NULL,

    -- Who the email goes to, fixed when the reminder is raised.
    recipient_id          BIGINT       REFERENCES team_members (id) ON DELETE SET NULL,
    recipient_email       TEXT,

    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    read_at               TIMESTAMPTZ,

    -- pending -> sending -> sent, or failed (retried with back-off), or skipped
    -- (nobody to send to, email turned off, not configured, or the event
    -- changed before it went out). email_detail says why.
    email_status          TEXT         NOT NULL,
    email_attempts        INTEGER      NOT NULL DEFAULT 0,
    email_next_attempt_at TIMESTAMPTZ,
    email_sent_at         TIMESTAMPTZ,
    email_detail          TEXT         NOT NULL DEFAULT '',

    CONSTRAINT notifications_once_per_reminder UNIQUE (task_id, remind_at),
    CONSTRAINT notifications_email_status_valid
        CHECK (email_status IN ('pending', 'sending', 'sent', 'failed', 'skipped'))
);

CREATE INDEX IF NOT EXISTS notifications_created_at_idx
    ON notifications (created_at DESC);

CREATE INDEX IF NOT EXISTS notifications_email_queue_idx
    ON notifications (email_next_attempt_at)
    WHERE email_status IN ('pending', 'sending', 'failed');

-- ---------------------------------------------------------------------------
-- Stage history: one row per stage a lead has been in
--
-- The first row of every lead has from_stage NULL: it is the stage the lead was
-- created in, at the moment it was created. Each later row is a move, written
-- in the same transaction as the stage change itself (and alongside its
-- timeline entry), so the history can never disagree with the lead.
--
-- Before this table, the only record was the text of stage_change timeline
-- notes. The INSERT below rebuilds history from those notes for every lead that
-- has none yet, so it is safe to re-run and only ever fills gaps.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS lead_stage_history (
    id          BIGSERIAL    PRIMARY KEY,
    lead_id     BIGINT       NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    from_stage  lead_stage,
    to_stage    lead_stage   NOT NULL,
    changed_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT lead_stage_history_is_a_change
        CHECK (from_stage IS NULL OR from_stage <> to_stage)
);

CREATE INDEX IF NOT EXISTS lead_stage_history_lead_idx
    ON lead_stage_history (lead_id, changed_at, id);

WITH labels (stage, label) AS (
    VALUES ('new'::lead_stage, 'New'),
           ('contacted',       'Contacted'),
           ('qualified',       'Qualified'),
           ('proposal_sent',   'Proposal Sent'),
           ('negotiation',     'Negotiation'),
           ('won',             'Won'),
           ('lost',            'Lost')
),
missing AS (
    SELECT l.id, l.created_at
    FROM leads l
    WHERE NOT EXISTS (SELECT 1 FROM lead_stage_history h WHERE h.lead_id = l.id)
)
INSERT INTO lead_stage_history (lead_id, from_stage, to_stage, changed_at)
SELECT lead_id, from_stage, to_stage, changed_at
FROM (
    -- Where each lead started: "Stage set to X." if it was created into a
    -- later stage, otherwise New.
    SELECT m.id AS lead_id,
           NULL::lead_stage AS from_stage,
           COALESCE(
               (SELECT lb.stage
                FROM activities a
                JOIN labels lb ON a.note = 'Stage set to ' || lb.label || '.'
                WHERE a.lead_id = m.id AND a.type = 'stage_change'
                ORDER BY a.created_at, a.id
                LIMIT 1),
               'new') AS to_stage,
           m.created_at AS changed_at,
           0::bigint AS activity_id
    FROM missing m

    UNION ALL

    -- Every move: "Stage changed from X to Y."
    SELECT a.lead_id, f.stage, t.stage, a.created_at, a.id
    FROM activities a
    JOIN missing m ON m.id = a.lead_id
    JOIN labels f ON true
    JOIN labels t ON a.note = 'Stage changed from ' || f.label || ' to ' || t.label || '.'
    WHERE a.type = 'stage_change'
      AND f.stage <> t.stage
) history
ORDER BY lead_id, changed_at, activity_id;

-- ---------------------------------------------------------------------------
-- Owners and editable timeline entries
--
-- owner_change rows are written by the server when a lead is given to someone
-- else, like stage_change. edited_at is set when someone corrects a note, call,
-- email or meeting they logged; system-written rows are never edited.
-- ---------------------------------------------------------------------------

ALTER TYPE activity_type ADD VALUE IF NOT EXISTS 'owner_change';

ALTER TABLE activities ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ;

-- The Leads page filters by owner ("my leads").
CREATE INDEX IF NOT EXISTS leads_owner_idx ON leads (owner_id);

-- ---------------------------------------------------------------------------
-- Deal alerts: "this deal is stuck"
--
-- Raised by the reminder job the first time a deal's stint in a stage passes
-- that stage's limit (model.stageStuckDays). One per stint: the unique key is
-- the lead plus when it entered the stage, so a deal that moves on and later
-- stalls again gets a fresh alert, and re-running never duplicates one.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS deal_alerts (
    id                BIGSERIAL    PRIMARY KEY,
    lead_id           BIGINT       NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    stage             lead_stage   NOT NULL,
    stage_entered_at  TIMESTAMPTZ  NOT NULL,
    limit_days        INTEGER      NOT NULL CHECK (limit_days > 0),
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    read_at           TIMESTAMPTZ,

    CONSTRAINT deal_alerts_once_per_stint UNIQUE (lead_id, stage_entered_at)
);

CREATE INDEX IF NOT EXISTS deal_alerts_created_at_idx
    ON deal_alerts (created_at DESC);

-- ---------------------------------------------------------------------------
-- Daily summary emails
--
-- One row per member per day, claimed by the reminder job before it sends, so
-- each person gets at most one summary a day even with two servers running.
-- A failed send is retried a few times; 'skipped' means there was nothing to
-- report that day.
-- ---------------------------------------------------------------------------

ALTER TABLE team_members ADD COLUMN IF NOT EXISTS daily_digest BOOLEAN NOT NULL DEFAULT true;

CREATE TABLE IF NOT EXISTS daily_digests (
    id           BIGSERIAL    PRIMARY KEY,
    member_id    BIGINT       NOT NULL REFERENCES team_members (id) ON DELETE CASCADE,
    digest_date  DATE         NOT NULL,
    status       TEXT         NOT NULL,
    attempts     INTEGER      NOT NULL DEFAULT 0,
    detail       TEXT         NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT daily_digests_once_per_day UNIQUE (member_id, digest_date),
    CONSTRAINT daily_digests_status_valid
        CHECK (status IN ('sending', 'sent', 'failed', 'skipped'))
);

-- ---------------------------------------------------------------------------
-- Email templates
--
-- Reusable emails with {{fields}} (model.TemplateFields) that the server fills
-- in from the lead. Subject and body are capped so a sent email, with its
-- "Subject:" line, always fits one timeline entry (2000 characters).
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS email_templates (
    id         BIGSERIAL    PRIMARY KEY,
    name       TEXT         NOT NULL,
    subject    TEXT         NOT NULL,
    body       TEXT         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT email_templates_name_not_blank
        CHECK (btrim(name) <> '' AND char_length(name) <= 120),
    CONSTRAINT email_templates_subject_valid
        CHECK (btrim(subject) <> '' AND char_length(subject) <= 200
               AND subject !~ '[\r\n]'),
    CONSTRAINT email_templates_body_valid
        CHECK (btrim(body) <> '' AND char_length(body) <= 1750)
);

-- Two templates called "Follow-up" and "follow-up" would be impossible to
-- tell apart in the picker.
CREATE UNIQUE INDEX IF NOT EXISTS email_templates_name_lower_key
    ON email_templates (lower(name));

-- Starter templates, only while there are none at all: a template someone
-- deleted is not brought back each time this file runs, unless every one is.
INSERT INTO email_templates (name, subject, body)
SELECT t.name, t.subject, t.body
FROM (VALUES
    ('Introduction',
     'Quick introduction, {{first_name}}',
     E'Hi {{first_name}},\n\nThanks for your interest. I''d love to learn more about what {{company}} is looking for and see whether we can help.\n\nWould you have 20 minutes for a quick call this week?\n\nBest regards,\n{{owner_name}}'),
    ('Follow-up after a call',
     'Great speaking with you, {{first_name}}',
     E'Hi {{first_name}},\n\nThank you for your time on the call today. As discussed, I''ll put together the next steps for {{company}} and share them shortly.\n\nIn the meantime, please reply here with any questions.\n\nBest regards,\n{{owner_name}}'),
    ('Checking in on the proposal',
     'Following up on our proposal for {{company}}',
     E'Hi {{first_name}},\n\nI wanted to check in on the proposal we sent over. Have you had a chance to review it with your team?\n\nI''m happy to walk through any part of it, or adjust it to fit what {{company}} needs.\n\nBest regards,\n{{owner_name}}')
) AS t(name, subject, body)
WHERE NOT EXISTS (SELECT 1 FROM email_templates);

-- ---------------------------------------------------------------------------
-- Win/loss reasons
--
-- Why a Won or Lost deal closed: one of model.WonReasons or model.LostReasons,
-- with an optional note (required for "other", which the API checks). Kept on
-- the lead while it is closed and cleared when it reopens; the timeline entry
-- for each stage change records the reason for good. Losing a deal requires
-- a reason, but only from now on: deals lost before this have none.
-- ---------------------------------------------------------------------------

ALTER TABLE leads ADD COLUMN IF NOT EXISTS close_reason TEXT;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS close_note   TEXT;

DO $$
BEGIN
    ALTER TABLE leads ADD CONSTRAINT leads_close_reason_valid CHECK (
        (close_reason IS NULL AND close_note IS NULL)
        OR (stage = 'lost' AND close_reason IN
               ('price', 'competitor', 'no_budget', 'timing', 'no_decision', 'product_fit', 'other'))
        OR (stage = 'won' AND close_reason IN
               ('product_fit', 'price', 'relationship', 'service', 'referral', 'other'))
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE leads ADD CONSTRAINT leads_close_note_length
        CHECK (close_note IS NULL OR (btrim(close_note) <> '' AND char_length(close_note) <= 500));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- ---------------------------------------------------------------------------
-- Automation rules
--
-- "When a new lead comes in (from these sources, worth at least this much),
-- plan this task for it." A rule runs inside the transaction that creates the
-- lead, so a lead never exists without the tasks its rules promised.
-- automation_runs records each run, at most once per rule and lead, for the
-- rule's run count and as the guard against planning the same task twice.
-- ---------------------------------------------------------------------------

-- Timeline entry written when a rule plans a task. Outside any transaction for
-- the same reason as the other ADD VALUEs.
ALTER TYPE activity_type ADD VALUE IF NOT EXISTS 'automation';

CREATE TABLE IF NOT EXISTS automation_rules (
    id             BIGSERIAL    PRIMARY KEY,
    name           TEXT         NOT NULL,
    enabled        BOOLEAN      NOT NULL DEFAULT true,
    -- Only 'lead_created' for now; a TEXT so later triggers need no new type.
    trigger_event  TEXT         NOT NULL DEFAULT 'lead_created',

    -- Conditions. An empty list means any source; NULL means any deal value.
    sources        TEXT[]       NOT NULL DEFAULT '{}',
    min_deal_value BIGINT,

    -- The task to plan. task_title may use the email template {{fields}}.
    task_title     TEXT         NOT NULL,
    task_type      TEXT         NOT NULL DEFAULT 'todo',
    due_in_days    INTEGER      NOT NULL DEFAULT 1,
    -- NULL gives the task to the lead's owner. Deleting the member falls back
    -- to the owner rather than deleting the rule.
    assignee_id    BIGINT       REFERENCES team_members (id) ON DELETE SET NULL,

    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT automation_rules_name_not_blank
        CHECK (btrim(name) <> '' AND char_length(name) <= 120),
    CONSTRAINT automation_rules_trigger_valid
        CHECK (trigger_event IN ('lead_created')),
    CONSTRAINT automation_rules_sources_valid
        CHECK (sources <@ ARRAY['website', 'referral', 'linkedin', 'cold_call', 'other']),
    CONSTRAINT automation_rules_min_deal_value_valid
        CHECK (min_deal_value IS NULL OR min_deal_value >= 0),
    CONSTRAINT automation_rules_task_title_not_blank
        CHECK (btrim(task_title) <> '' AND char_length(task_title) <= 200),
    CONSTRAINT automation_rules_task_type_valid
        CHECK (task_type IN ('todo', 'call', 'email', 'meeting', 'demo', 'follow_up')),
    CONSTRAINT automation_rules_due_in_days_valid
        CHECK (due_in_days BETWEEN 0 AND 365)
);

CREATE UNIQUE INDEX IF NOT EXISTS automation_rules_name_lower_key
    ON automation_rules (lower(name));

CREATE TABLE IF NOT EXISTS automation_runs (
    id      BIGSERIAL    PRIMARY KEY,
    rule_id BIGINT       NOT NULL REFERENCES automation_rules (id) ON DELETE CASCADE,
    lead_id BIGINT       NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    -- The task it planned; kept as a run when someone deletes the task.
    task_id BIGINT       REFERENCES tasks (id) ON DELETE SET NULL,
    ran_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT automation_runs_once_per_lead UNIQUE (rule_id, lead_id)
);

-- Starter rules, switched off, only while there are none at all (as for the
-- email templates).
INSERT INTO automation_rules (name, enabled, sources, task_title, task_type, due_in_days)
SELECT r.name, false, r.sources, r.task_title, r.task_type, r.due_in_days
FROM (VALUES
    ('Intro call for every new lead', '{}'::text[], 'Intro call with {{first_name}}', 'call', 1),
    ('Same-day reply to website leads', '{website}'::text[], 'Reply to {{first_name}}''s enquiry', 'email', 0)
) AS r(name, sources, task_title, task_type, due_in_days)
WHERE NOT EXISTS (SELECT 1 FROM automation_rules);

-- ---------------------------------------------------------------------------
-- Ad lead forms
--
-- Leads from Meta Lead Ads (Facebook / Instagram) and Google Ads lead forms,
-- which each send a webhook when someone fills in the form. ad_leads records
-- every one received, whatever became of it, so the Integrations page can show
-- what arrived and why any failed, and so a lead the platform sends twice is
-- only taken once (the unique key on provider and the platform's own id).
-- ---------------------------------------------------------------------------

-- Outside any transaction for the same reason as the other ADD VALUEs.
ALTER TYPE lead_source ADD VALUE IF NOT EXISTS 'meta_ads';
ALTER TYPE lead_source ADD VALUE IF NOT EXISTS 'google_ads';

-- Timeline entry for a lead that filled in an ad form again.
ALTER TYPE activity_type ADD VALUE IF NOT EXISTS 'ad_form';

-- The new sources may be automation rule conditions too.
ALTER TABLE automation_rules DROP CONSTRAINT IF EXISTS automation_rules_sources_valid;
ALTER TABLE automation_rules ADD CONSTRAINT automation_rules_sources_valid
    CHECK (sources <@ ARRAY['website', 'referral', 'linkedin', 'cold_call', 'other', 'meta_ads', 'google_ads']);

CREATE TABLE IF NOT EXISTS ad_leads (
    id          BIGSERIAL    PRIMARY KEY,
    provider    TEXT         NOT NULL,
    -- The platform's id for the submission: Meta's leadgen_id, Google's lead_id.
    external_id TEXT         NOT NULL,
    form_id     TEXT         NOT NULL DEFAULT '',
    campaign    TEXT         NOT NULL DEFAULT '',
    ad          TEXT         NOT NULL DEFAULT '',
    -- A platform's test submission: recorded, never made into a lead.
    is_test     BOOLEAN      NOT NULL DEFAULT false,

    -- created: a new lead; duplicate: its email was already a lead, which got
    -- a timeline note; test: see is_test; failed: see error.
    status      TEXT         NOT NULL,
    error       TEXT         NOT NULL DEFAULT '',
    lead_id     BIGINT       REFERENCES leads (id) ON DELETE SET NULL,

    -- The answers as the form gave them: [{key, label, value}], in form order.
    answers     JSONB        NOT NULL DEFAULT '[]',
    received_at TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT ad_leads_provider_valid CHECK (provider IN ('meta', 'google')),
    CONSTRAINT ad_leads_status_valid CHECK (status IN ('created', 'duplicate', 'test', 'failed')),
    CONSTRAINT ad_leads_once UNIQUE (provider, external_id)
);

-- The Integrations page's "latest received" list.
CREATE INDEX IF NOT EXISTS ad_leads_received_at_idx ON ad_leads (received_at DESC);

-- ---------------------------------------------------------------------------
-- Optional: trigram indexes for the search box
--
-- Search is "ILIKE '%query%'", which a normal B-tree index cannot accelerate
-- because of the leading wildcard. pg_trgm + GIN can.
--
-- It ships with postgresql-contrib and needs privileges to install, so this is
-- wrapped in exception handling: if the extension is unavailable the schema
-- still applies cleanly and search simply falls back to a sequential scan
-- (entirely fine at this data size).
-- ---------------------------------------------------------------------------

DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_trgm;
EXCEPTION
    WHEN insufficient_privilege OR undefined_file OR feature_not_supported THEN
        RAISE NOTICE 'pg_trgm not available - skipping trigram search indexes';
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm') THEN
        EXECUTE 'CREATE INDEX IF NOT EXISTS leads_name_trgm_idx
                     ON leads USING gin (name gin_trgm_ops)';
        EXECUTE 'CREATE INDEX IF NOT EXISTS leads_company_trgm_idx
                     ON leads USING gin (company gin_trgm_ops)';
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- Meta Ads: settings, attribution, lead quality and conversion feedback
--
-- integration_settings holds what the Meta Ads settings page saves. The
-- credentials are in `secrets`, sealed with AES-256-GCM by the API using
-- SECRETS_ENCRYPTION_KEY; the database never sees them in the clear.
-- secret_hints keeps only the masked form ("••••1234") to show on the page.
-- ---------------------------------------------------------------------------

-- Outside any transaction for the same reason as the other ADD VALUEs.
-- A lead's quality was overridden by hand, or the override was removed.
ALTER TYPE activity_type ADD VALUE IF NOT EXISTS 'quality_change';

CREATE TABLE IF NOT EXISTS integration_settings (
    provider        TEXT         PRIMARY KEY,
    -- Non-secret settings: dataset id, ad account, page, forms, event mapping.
    config          JSONB        NOT NULL DEFAULT '{}',
    secrets         BYTEA,
    secret_hints    JSONB        NOT NULL DEFAULT '{}',
    -- not_connected: never tested, or disconnected; connected: the last Test
    -- Connection passed; error: it, or a later call to Meta, failed.
    status          TEXT         NOT NULL DEFAULT 'not_connected',
    last_tested_at  TIMESTAMPTZ,
    last_sync_at    TIMESTAMPTZ,  -- last sync that finished without an error
    last_sync_count INTEGER,
    last_error      TEXT         NOT NULL DEFAULT '',
    last_error_at   TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT integration_settings_provider_valid CHECK (provider IN ('meta')),
    CONSTRAINT integration_settings_status_valid
        CHECK (status IN ('not_connected', 'connected', 'error'))
);

-- Where a lead from Meta came from. One row per lead: the first form it filled
-- in is its attribution; filling one in again only adds a timeline note.
CREATE TABLE IF NOT EXISTS meta_lead_attribution (
    lead_id         BIGINT       PRIMARY KEY REFERENCES leads (id) ON DELETE CASCADE,
    meta_lead_id    TEXT         NOT NULL,
    page_id         TEXT         NOT NULL DEFAULT '',
    form_id         TEXT         NOT NULL DEFAULT '',
    ad_account_id   TEXT         NOT NULL DEFAULT '',
    campaign_id     TEXT         NOT NULL DEFAULT '',
    campaign_name   TEXT         NOT NULL DEFAULT '',
    adset_id        TEXT         NOT NULL DEFAULT '',
    adset_name      TEXT         NOT NULL DEFAULT '',
    ad_id           TEXT         NOT NULL DEFAULT '',
    ad_name         TEXT         NOT NULL DEFAULT '',
    is_organic      BOOLEAN      NOT NULL DEFAULT false,
    -- When the person submitted the form, per Meta (created_time).
    meta_created_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT meta_lead_attribution_meta_lead_once UNIQUE (meta_lead_id)
);

-- The same attribution, kept on the received submission too, so a failed one
-- that is retried later still knows where it came from.
ALTER TABLE ad_leads ADD COLUMN IF NOT EXISTS attribution JSONB;

CREATE INDEX IF NOT EXISTS meta_lead_attribution_form_idx     ON meta_lead_attribution (form_id);
CREATE INDEX IF NOT EXISTS meta_lead_attribution_campaign_idx ON meta_lead_attribution (campaign_id);
CREATE INDEX IF NOT EXISTS meta_lead_attribution_created_idx  ON meta_lead_attribution (meta_created_at DESC);

-- Lead quality: separate from the stage (where the deal is) and from the
-- derived pipeline score (how active it is). quality_score is worked out from
-- the configurable rules when the lead arrives; quality_override replaces it
-- when someone with the admin key disagrees, and needs a reason. quality is the
-- band of whichever applies, stored so the dashboard can filter on it.
ALTER TABLE leads ADD COLUMN IF NOT EXISTS quality                 TEXT NOT NULL DEFAULT 'not_evaluated';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS quality_score           INTEGER;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS quality_breakdown       JSONB;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS quality_override        INTEGER;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS quality_override_reason TEXT;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS quality_override_at     TIMESTAMPTZ;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS qualification_reason    TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    ALTER TABLE leads ADD CONSTRAINT leads_quality_valid
        CHECK (quality IN ('good', 'average', 'poor', 'not_evaluated'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE leads ADD CONSTRAINT leads_quality_scores_range
        CHECK ((quality_score IS NULL OR quality_score BETWEEN 0 AND 100)
           AND (quality_override IS NULL OR quality_override BETWEEN 0 AND 100));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- An override always comes with its reason, and a reason only with one.
DO $$
BEGIN
    ALTER TABLE leads ADD CONSTRAINT leads_quality_override_reason
        CHECK ((quality_override IS NULL) = (quality_override_reason IS NULL)
           AND (quality_override_reason IS NULL OR
                (btrim(quality_override_reason) <> '' AND char_length(quality_override_reason) <= 500)));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE leads ADD CONSTRAINT leads_qualification_reason_length
        CHECK (char_length(qualification_reason) <= 1000);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS leads_quality_idx ON leads (quality);

-- Settings that are neither a credential nor per integration: the lead
-- quality rules, as {"rules": [...], "good_from": 70, "average_from": 40}.
CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT         PRIMARY KEY,
    value      JSONB        NOT NULL,
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Conversion events for Meta's Conversions API: an outbox. A stage change
-- the mapping says matters writes a row here in the same transaction; the
-- sender delivers it, retrying with backoff. The unique key makes queueing
-- idempotent: a lead sends each event once per mode however often it moves in
-- and out of the stage, and event_id lets Meta drop a duplicate delivery.
CREATE TABLE IF NOT EXISTS capi_events (
    id              BIGSERIAL    PRIMARY KEY,
    lead_id         BIGINT       NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    meta_lead_id    TEXT         NOT NULL,
    stage           lead_stage   NOT NULL,
    event_name      TEXT         NOT NULL,
    event_id        TEXT         NOT NULL,
    event_time      TIMESTAMPTZ  NOT NULL,
    -- Sent with the test event code: shows in Events Manager's Test Events
    -- tab only, and never counts towards ad delivery.
    test_mode       BOOLEAN      NOT NULL,
    status          TEXT         NOT NULL DEFAULT 'pending',
    attempts        INTEGER      NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- What went wrong last, safe to show: Meta's message, never a token.
    last_error      TEXT         NOT NULL DEFAULT '',
    fbtrace_id      TEXT         NOT NULL DEFAULT '',
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT capi_events_status_valid
        CHECK (status IN ('pending', 'sent', 'failed', 'retry_scheduled')),
    CONSTRAINT capi_events_event_id_once UNIQUE (event_id),
    CONSTRAINT capi_events_once UNIQUE (lead_id, event_name, test_mode)
);

-- The sender's "what is due" scan.
CREATE INDEX IF NOT EXISTS capi_events_due_idx
    ON capi_events (next_attempt_at) WHERE status IN ('pending', 'retry_scheduled');
CREATE INDEX IF NOT EXISTS capi_events_lead_idx ON capi_events (lead_id);

-- ---------------------------------------------------------------------------
-- Meta CAPI, second round: consent, spam, quality reasons, error codes and an
-- audit trail of credential changes.
-- ---------------------------------------------------------------------------

-- meta_share_opt_out: never send this lead's events to Meta (consent).
-- spam_confirmed: a person confirmed the lead's data is invalid or spam; its
-- quality is Poor and it never sends a Qualified or Converted event.
ALTER TABLE leads ADD COLUMN IF NOT EXISTS meta_share_opt_out BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS spam_confirmed     BOOLEAN NOT NULL DEFAULT false;
-- Why the lead has the quality it has, in words, and when it was worked out.
ALTER TABLE leads ADD COLUMN IF NOT EXISTS quality_reason     TEXT NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS quality_updated_at TIMESTAMPTZ;

-- Meta's error code for the last failed attempt (190 a bad token, 200 a
-- missing permission ...), and 'cancelled' for an event that must no longer
-- be sent (the lead opted out, or was confirmed spam, after it was queued).
ALTER TABLE capi_events ADD COLUMN IF NOT EXISTS error_code INTEGER;
ALTER TABLE capi_events DROP CONSTRAINT IF EXISTS capi_events_status_valid;
ALTER TABLE capi_events ADD CONSTRAINT capi_events_status_valid
    CHECK (status IN ('pending', 'sent', 'failed', 'retry_scheduled', 'cancelled'));

-- The Conversions API's own health, separate from the lead-reading side:
-- connected, invalid_credentials, permission_denied, temporarily_unavailable,
-- or NULL until something has been sent or tested. "Not configured" and
-- "Disabled" are worked out from the settings, not stored.
ALTER TABLE integration_settings ADD COLUMN IF NOT EXISTS capi_status         TEXT;
ALTER TABLE integration_settings ADD COLUMN IF NOT EXISTS capi_status_detail  TEXT NOT NULL DEFAULT '';
ALTER TABLE integration_settings ADD COLUMN IF NOT EXISTS capi_status_at      TIMESTAMPTZ;
ALTER TABLE integration_settings ADD COLUMN IF NOT EXISTS capi_token_rotated_at TIMESTAMPTZ;

-- Who changed integration settings, and what. Never a secret: a replaced
-- token is recorded as "replaced", not by value.
CREATE TABLE IF NOT EXISTS audit_log (
    id         BIGSERIAL    PRIMARY KEY,
    action     TEXT         NOT NULL,
    -- The CRM has no user accounts; the actor is "admin key" plus the
    -- caller's address, which is as much as can honestly be said.
    actor      TEXT         NOT NULL,
    detail     JSONB        NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_log_created_idx ON audit_log (created_at DESC);

-- ---------------------------------------------------------------------------
-- Login: team members become user accounts
--
-- A team member is the person leads are assigned to (leads.owner_id). They
-- become an account by having a role and a password. Existing members keep
-- their id and their leads; with no password set they cannot log in until an
-- admin sets one. Roles: admin (everything), manager (every lead, assigning),
-- sales (only the leads assigned to them).
-- ---------------------------------------------------------------------------

ALTER TABLE team_members ADD COLUMN IF NOT EXISTS role          TEXT NOT NULL DEFAULT 'sales';
-- bcrypt hash; NULL means the member cannot log in.
ALTER TABLE team_members ADD COLUMN IF NOT EXISTS password_hash TEXT;
-- An inactive member cannot log in, and their sessions end.
ALTER TABLE team_members ADD COLUMN IF NOT EXISTS active        BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE team_members ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;

DO $$
BEGIN
    ALTER TABLE team_members ADD CONSTRAINT team_members_role_valid
        CHECK (role IN ('admin', 'manager', 'sales'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- One row per logged-in browser. The cookie holds a random token; only its
-- SHA-256 is stored, so a database leak does not hand out live sessions.
CREATE TABLE IF NOT EXISTS sessions (
    id           BIGSERIAL    PRIMARY KEY,
    token_hash   BYTEA        NOT NULL,
    member_id    BIGINT       NOT NULL REFERENCES team_members (id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- Ends at expires_at, or after a stretch of not being used.
    expires_at   TIMESTAMPTZ  NOT NULL,
    last_seen_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ip           TEXT         NOT NULL DEFAULT '',
    user_agent   TEXT         NOT NULL DEFAULT '',

    CONSTRAINT sessions_token_once UNIQUE (token_hash)
);
CREATE INDEX IF NOT EXISTS sessions_member_idx ON sessions (member_id);
-- Lead lists for one salesperson.
CREATE INDEX IF NOT EXISTS leads_owner_idx ON leads (owner_id);

-- Sign up: someone who signed themselves up is inactive and waits here until
-- an admin approves them (switches them on), so a stranger who finds the page
-- cannot see any lead.
ALTER TABLE team_members ADD COLUMN IF NOT EXISTS signup_pending BOOLEAN NOT NULL DEFAULT false;

-- Forgot password: a one-time link, emailed, good for an hour. Only the
-- token's SHA-256 is stored, as for sessions.
CREATE TABLE IF NOT EXISTS password_resets (
    id          BIGSERIAL    PRIMARY KEY,
    token_hash  BYTEA        NOT NULL,
    member_id   BIGINT       NOT NULL REFERENCES team_members (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ  NOT NULL,
    used_at     TIMESTAMPTZ,

    CONSTRAINT password_resets_token_once UNIQUE (token_hash)
);
CREATE INDEX IF NOT EXISTS password_resets_member_idx ON password_resets (member_id);

-- Customer links: pages a lead opens without logging in. "booking" lets them
-- pick a free slot in their salesperson's calendar; "feedback" is the
-- thank-you page a won customer gets, to rate the deal and refer a friend.
-- The token is the whole credential, kept in plain text so the salesperson
-- can copy the link again; it opens one lead's page and nothing else.
-- Outside any transaction for the same reason as the other ADD VALUEs.
ALTER TYPE activity_type ADD VALUE IF NOT EXISTS 'booking';
ALTER TYPE activity_type ADD VALUE IF NOT EXISTS 'feedback';

CREATE TABLE IF NOT EXISTS customer_links (
    id         BIGSERIAL    PRIMARY KEY,
    lead_id    BIGINT       NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    kind       TEXT         NOT NULL,
    token      TEXT         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- When the link was emailed to the lead; NULL if it never was.
    emailed_at TIMESTAMPTZ,
    -- The meeting last booked with it. While that meeting is still ahead, the
    -- link shows it instead of offering another slot.
    booked_task_id BIGINT REFERENCES tasks (id) ON DELETE SET NULL,

    CONSTRAINT customer_links_kind_valid CHECK (kind IN ('booking', 'feedback')),
    CONSTRAINT customer_links_token_once UNIQUE (token),
    CONSTRAINT customer_links_one_per_kind UNIQUE (lead_id, kind)
);

-- What a won customer said on the feedback page. Once per lead.
CREATE TABLE IF NOT EXISTS lead_feedback (
    id               BIGSERIAL    PRIMARY KEY,
    lead_id          BIGINT       NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    rating           SMALLINT     NOT NULL,
    comment          TEXT         NOT NULL DEFAULT '',
    -- The friend they referred, made into a new lead.
    referral_lead_id BIGINT       REFERENCES leads (id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT lead_feedback_once UNIQUE (lead_id),
    CONSTRAINT lead_feedback_rating_valid CHECK (rating BETWEEN 1 AND 5),
    CONSTRAINT lead_feedback_comment_length CHECK (char_length(comment) <= 2000)
);

-- ---------------------------------------------------------------------------
-- Google Sheets: leads exported to a spreadsheet an admin picks.
--
-- The settings live in integration_settings under provider 'google_sheets':
-- config holds the spreadsheet, tab, columns and auto-sync choice; secrets
-- holds the OAuth refresh token, sealed with SECRETS_ENCRYPTION_KEY like
-- Meta's. Access tokens are never stored: they last an hour and are fetched
-- with the refresh token when needed.
-- ---------------------------------------------------------------------------

ALTER TABLE integration_settings DROP CONSTRAINT IF EXISTS integration_settings_provider_valid;
ALTER TABLE integration_settings ADD CONSTRAINT integration_settings_provider_valid
    CHECK (provider IN ('meta', 'google_sheets'));

-- One row per "Connect Google Account" click, until Google sends the admin
-- back. Only the state's SHA-256 is stored; it is single-use, expires after
-- ten minutes, and belongs to the admin who started it. code_verifier is the
-- PKCE secret for the same exchange.
CREATE TABLE IF NOT EXISTS google_oauth_states (
    state_hash    BYTEA        PRIMARY KEY,
    member_id     BIGINT       NOT NULL REFERENCES team_members (id) ON DELETE CASCADE,
    code_verifier TEXT         NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ  NOT NULL
);

-- Each export to the sheet: what it did, and what went wrong.
CREATE TABLE IF NOT EXISTS sheet_syncs (
    id            BIGSERIAL    PRIMARY KEY,
    trigger       TEXT         NOT NULL,
    status        TEXT         NOT NULL DEFAULT 'running',
    started_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    leads_total   INTEGER      NOT NULL DEFAULT 0,
    rows_updated  INTEGER      NOT NULL DEFAULT 0,
    rows_added    INTEGER      NOT NULL DEFAULT 0,
    rows_failed   INTEGER      NOT NULL DEFAULT 0,
    error         TEXT         NOT NULL DEFAULT '',
    -- Who pressed Sync Now or Retry; NULL for automatic syncs.
    started_by    BIGINT       REFERENCES team_members (id) ON DELETE SET NULL,
    -- A failed sync that a later one retried.
    retry_of      BIGINT       REFERENCES sheet_syncs (id) ON DELETE SET NULL,

    CONSTRAINT sheet_syncs_trigger_valid CHECK (trigger IN ('manual', 'auto', 'retry')),
    CONSTRAINT sheet_syncs_status_valid
        CHECK (status IN ('running', 'success', 'partial', 'failed'))
);
CREATE INDEX IF NOT EXISTS sheet_syncs_started_idx ON sheet_syncs (started_at DESC);
