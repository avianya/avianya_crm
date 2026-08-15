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
