# Pipeline — CRM Lead Management

Lead management and lifecycle tracking. **React + TypeScript** frontend, **Go** REST API, **PostgreSQL** database.

No authentication — the app opens straight to the dashboard and all data is shared, as specified.

> **Just cloned this and want to run it?** → **[`SETUP.md`](./SETUP.md)** walks you
> through it from scratch — installing Go, creating the database, starting all
> three pieces — assuming no Go knowledge at all.
>
> **Want to understand the code?** → [`DOCUMENTATION.md`](./DOCUMENTATION.md) is a Go
> primer written for someone who has never used it, followed by a file-by-file
> explanation of everything in this repo and how it is wired together.

---

## Contents

- [What it does](#what-it-does)
- [Tech stack](#tech-stack)
- [Running it](#running-it)
  - [Option A — Docker (one command)](#option-a--docker-one-command)
  - [Option B — Locally](#option-b--locally)
- [Project layout](#project-layout)
- [API reference](#api-reference)
- [Database schema](#database-schema)
- [Tests](#tests)
- [Design decisions](#design-decisions)
- [Assumptions](#assumptions)
- [What I would do with more time](#what-i-would-do-with-more-time)

---

## What it does

**Dashboard**
- Total leads, conversion rate, won/lost counts, leads added in the last 7 days
- Pipeline-by-stage bar chart — every bar is a link into the filtered list
- The 5 most recently added leads

**Leads list**
- Table on desktop, cards on mobile
- Search by name or company (debounced, case-insensitive)
- Filter by stage (always-visible chips) and by source
- Sort by date added, last updated, name, company or stage — in either direction
- Change a lead's stage inline, without leaving the list
- Paginated, 10 per page
- Every filter lives in the URL, so views are shareable and the back button works

**Add / edit lead**
- Full name, company, email, phone, source, stage, notes
- Validated on both sides; server-side field errors land on the right input
- A duplicate email returns 409 and is shown on the email field

**Lead detail**
- Full profile with clickable email and phone
- Step indicator for the lifecycle — click a step to move the lead there
- "Advance to <next stage>" one-click action
- Activity timeline, newest first
- Log a note, call, email or meeting (Ctrl/Cmd+Enter to save)
- Stage changes are written to the timeline **automatically**, in the same database transaction as the change itself

**Throughout**
- Light / dark / follow-the-system theme
- Skeleton loaders, designed empty states, toast confirmations, keyboard-accessible dialogs and menus

---

## Tech stack

| Layer | Choice | Why |
|---|---|---|
| Frontend | React 19 + TypeScript, Vite | Fast dev server; TS catches the null/undefined mistakes a nullable `phone` invites |
| Styling | Tailwind CSS v4 | Configured entirely from CSS; no config file to keep in sync |
| Routing | React Router 7 | Filter state in the URL means shareable, bookmarkable views |
| Charts | Hand-built with `div`s | Seven bars did not justify a charting dependency, and this keeps the stage colours consistent with the rest of the app |
| Icons | lucide-react | Tree-shakeable |
| Backend | Go 1.26 + Gin | Small, explicit HTTP layer |
| DB driver | pgx/v5 | The best-maintained Postgres driver for Go; native support for the features this schema uses |
| SQL | Hand-written, no ORM | The whole app is filtering, aggregating and paginating — worth seeing the exact query that runs |
| Database | PostgreSQL 17 | Enums, partial/functional indexes, triggers, window functions |

**Runtime dependencies are deliberately few.** The backend has three direct dependencies (Gin, pgx, godotenv); the frontend has three (React, React Router, lucide-react).

---

## Running it

### Option A — Docker (one command)

The only prerequisite is Docker with Compose v2.

```bash
docker compose up --build
```

Then open **http://localhost:5173**.

| Service | URL |
|---|---|
| Frontend | http://localhost:5173 |
| API | http://localhost:8080/api |
| PostgreSQL | `localhost:5433` (user `crm`, password `crm_password`, db `crm_leads`) |

The schema and demo data are applied automatically on first start. Postgres is
published on **5433** so it will not collide with a PostgreSQL you already have
installed.

```bash
docker compose down       # stop, keep the data
docker compose down -v    # stop and wipe the database (re-seeds on next start)
```

> The database is only initialised when its volume is empty. If you change
> `db/schema.sql`, run `docker compose down -v` to pick it up.

---

### Option B — Locally

**Prerequisites**

| Tool | Version | Check |
|---|---|---|
| Go | 1.24+ | `go version` |
| Node.js | 20+ | `node --version` |
| PostgreSQL | 14+ | `psql --version` |

<details>
<summary><b>Installing Go, if you do not have it</b></summary>

`go version` failing means Go is not installed. **Do not use `apt install golang-go`** — distribution packages lag well behind.

```bash
# 1. Download the current release (check https://go.dev/dl for the latest)
curl -LO https://go.dev/dl/go1.26.6.linux-amd64.tar.gz

# 2. Unpack it into your home directory (no sudo needed)
mkdir -p ~/.local
rm -rf ~/.local/go
tar -C ~/.local -xzf go1.26.6.linux-amd64.tar.gz

# 3. Put it on your PATH permanently
echo 'export PATH=$HOME/.local/go/bin:$PATH' >> ~/.zshrc   # or ~/.bashrc
source ~/.zshrc

# 4. Verify
go version    # go version go1.26.6 linux/amd64
```

macOS: `brew install go`. Windows: the installer from https://go.dev/dl.
</details>

---

#### 1. Database

```bash
# Create the database
createdb crm_leads
# ...or, if your setup needs an explicit superuser:
psql -U postgres -h localhost -c "CREATE DATABASE crm_leads;"

# Apply the schema (safe to re-run — it is idempotent)
psql -U postgres -h localhost -d crm_leads -f db/schema.sql

# Load 14 demo leads and 29 activities (optional but recommended)
psql -U postgres -h localhost -d crm_leads -f db/seed.sql
```

> `seed.sql` **truncates** `leads` and `activities` first, so it always produces
> the same known dataset. Never run it against data you care about.

#### 2. Backend

```bash
cd backend
cp .env.example .env      # then edit DATABASE_URL if your credentials differ
go mod download
go run ./cmd/server
```

Serving on **http://localhost:8080**. Confirm it:

```bash
curl http://localhost:8080/api/health
# {"data":{"database":"ok","status":"ok"}}
```

> Run it from the `backend/` directory — `.env` is read from the working directory.

#### 3. Frontend

In a second terminal:

```bash
cd frontend
cp .env.example .env
npm install
npm run dev
```

Open **http://localhost:5173**.

---

#### Environment variables

`backend/.env` — see [`backend/.env.example`](./backend/.env.example)

| Variable | Default | Purpose |
|---|---|---|
| `APP_ENV` | `development` | `development` = text logs + internal error details in responses. `production` = JSON logs, errors hidden from clients |
| `PORT` | `8080` | Port the API listens on |
| `DATABASE_URL` | — | **Required.** `postgres://user:pass@host:port/db?sslmode=disable` |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173,...` | Comma-separated browser origins allowed to call the API |

`frontend/.env` — see [`frontend/.env.example`](./frontend/.env.example)

| Variable | Default | Purpose |
|---|---|---|
| `VITE_API_BASE_URL` | `http://localhost:8080/api` | Base URL of the API, including `/api` |

---

#### Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `DATABASE_URL is required` | No `.env` in the working directory. `cd backend && cp .env.example .env` |
| `ping database: ... connection refused` | Postgres is not running. `sudo systemctl start postgresql` |
| `password authentication failed` | Wrong credentials in `DATABASE_URL`. Percent-encode any `@ : / ?` in the password |
| `relation "leads" does not exist` | Schema not applied. `psql ... -f db/schema.sql` |
| `bind: address already in use` | Port 8080 taken. `PORT=8081 go run ./cmd/server` (and update `VITE_API_BASE_URL`) |
| "Cannot reach the API" in the UI | Backend is down, or `VITE_API_BASE_URL` points at the wrong port |
| Browser console: blocked by CORS | The frontend's origin is not in `CORS_ALLOWED_ORIGINS`. Restart the backend after changing it |
| Frontend `.env` change ignored | Vite reads `.env` at startup — restart `npm run dev` |
| `go: command not found` | Go is not on your PATH — see the install box above |

---

## Project layout

```
.
├── docker-compose.yml          Whole stack in one command
├── README.md                   This file
├── DOCUMENTATION.md            Go primer + full code walkthrough
│
├── db/
│   ├── schema.sql              Tables, enums, indexes, constraints, trigger
│   └── seed.sql                14 demo leads, 29 activities
│
├── backend/                    Go REST API
│   ├── cmd/server/main.go      Entry point: wiring + graceful shutdown
│   ├── internal/
│   │   ├── config/             Environment loading
│   │   ├── model/              Domain types + validation rules  (+ tests)
│   │   ├── store/              All SQL lives here              (+ tests)
│   │   └── api/                Routing, middleware, handlers
│   ├── Dockerfile
│   └── .env.example
│
└── frontend/                   React + TypeScript
    ├── src/
    │   ├── lib/                api.ts (HTTP), types.ts, display.ts
    │   ├── hooks/              useAsync, useDebouncedValue
    │   ├── context/            Theme, Toasts (hooks split from providers
    │   │                       so Fast Refresh keeps working)
    │   ├── components/         UI primitives + feature components
    │   ├── pages/              Dashboard, Leads, Lead detail
    │   ├── App.tsx             Route table
    │   └── index.css           Tailwind theme + design tokens
    ├── Dockerfile
    ├── nginx.conf
    └── .env.example
```

The backend's dependencies flow one way — `api → store → model` — and `model`
imports nothing from the other two. That is what lets the validation rules be
unit-tested with no database and no HTTP server.

---

## API reference

Base URL `http://localhost:8080/api`. Every response has one of two shapes:

```jsonc
// success
{ "data": ... }                              // + "pagination" on list endpoints

// failure
{ "error": { "code": "...", "message": "...", "details": { "field": "..." } } }
```

| Method | Path | Purpose | Success |
|---|---|---|---|
| `GET` | `/health` | Service + database liveness | 200 |
| `GET` | `/meta` | Stage / source / activity-type vocabularies with labels | 200 |
| `GET` | `/dashboard` | Aggregate summary | 200 |
| `GET` | `/leads` | List with filters, sort, pagination | 200 |
| `POST` | `/leads` | Create a lead | 201 + `Location` |
| `GET` | `/leads/:id` | One lead **with its full activity timeline** | 200 |
| `PUT` | `/leads/:id` | Replace the editable fields | 200 |
| `PATCH` | `/leads/:id/stage` | Change stage only (inline dropdown) | 200 |
| `DELETE` | `/leads/:id` | Delete a lead and its activities | 204 |
| `GET` | `/leads/:id/activities` | The timeline alone | 200 |
| `POST` | `/leads/:id/activities` | Log a note / call / email / meeting | 201 |

**`GET /leads` query parameters**

| Parameter | Values | Default |
|---|---|---|
| `search` (or `q`) | matches name **or** company, case-insensitive | — |
| `stage` | `new` `contacted` `qualified` `proposal_sent` `negotiation` `won` `lost` \| `all` | `all` |
| `source` | `website` `referral` `linkedin` `cold_call` `other` \| `all` | `all` |
| `sort_by` | `created_at` `updated_at` `name` `company` `stage` | `created_at` |
| `sort_dir` | `asc` `desc` | `desc` |
| `page` | 1-based | `1` |
| `page_size` | 1–100 (capped) | `10` |

An unknown `stage`, `source` or `sort_by` returns **422** rather than being
ignored — a typo in a filter is a bug worth surfacing. Out-of-range pagination is
clamped instead, because `?page=0` has an obvious sane reading.

**Status codes**

| Code | When |
|---|---|
| `200` / `201` / `204` | Success / created / deleted |
| `400` | Malformed JSON, or a non-numeric `:id` |
| `404` | No such lead, or no such endpoint |
| `405` | Wrong HTTP method for that path |
| `409` | Email already belongs to another lead |
| `422` | Valid JSON, invalid data — `details` names each bad field |
| `500` | Server fault. Full error is logged; the client gets a generic message |
| `503` | `/health` only: the database is unreachable |

<details>
<summary><b>Example requests</b></summary>

```bash
# Qualified leads at "peak", newest first
curl "http://localhost:8080/api/leads?search=peak&stage=qualified"

# Create
curl -X POST http://localhost:8080/api/leads \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada Lovelace","company":"Analytical Engines",
       "email":"ada@example.com","phone":"+91 90000 00000",
       "source":"referral","stage":"new","notes":"Inbound."}'

# Move along the pipeline (also writes a timeline entry)
curl -X PATCH http://localhost:8080/api/leads/1/stage \
  -H 'Content-Type: application/json' -d '{"stage":"qualified"}'

# Log a call
curl -X POST http://localhost:8080/api/leads/1/activities \
  -H 'Content-Type: application/json' \
  -d '{"type":"call","note":"Walked through the proposal."}'

# Validation failure — note the per-field details
curl -X POST http://localhost:8080/api/leads \
  -H 'Content-Type: application/json' -d '{"name":"","email":"nope"}'
```
</details>

---

## Database schema

```
leads                                   activities
├── id           BIGSERIAL PK           ├── id         BIGSERIAL PK
├── name         TEXT NOT NULL          ├── lead_id    BIGINT FK ──┐ ON DELETE
├── company      TEXT NOT NULL          ├── type       activity_type│  CASCADE
├── email        TEXT NOT NULL UNIQUE   ├── note       TEXT NOT NULL│
│                (on lower(email))      └── created_at TIMESTAMPTZ  │
├── phone        TEXT NULL                                         │
├── source       lead_source            leads.id ◄─────────────────┘
├── stage        lead_stage
├── notes        TEXT NOT NULL DEFAULT ''
├── created_at   TIMESTAMPTZ
└── updated_at   TIMESTAMPTZ  ← maintained by trigger
```

**Three enum types** — `lead_stage`, `lead_source`, `activity_type` — so invalid
values are rejected by the database, not only by the API.

`lead_stage` is declared in **lifecycle order**, and a Postgres enum sorts by
declaration order rather than alphabetically. `ORDER BY stage` therefore means
"order by pipeline progress" for free, with no `sort_order` column to keep in
sync. `?sort_by=stage` uses exactly this.

**Nine indexes**, each for a query the app actually runs:

| Index | Serves |
|---|---|
| `leads_pkey`, `activities_pkey` | Primary keys |
| `leads_email_lower_key` (unique, functional) | Case-insensitive email uniqueness → 409 |
| `leads_stage_created_at_idx` (composite) | "Filter by stage, newest first" — one index for both the `WHERE` and the `ORDER BY` |
| `leads_created_at_idx` | Unfiltered list; dashboard's recent-5 |
| `leads_source_idx` | Source filter |
| `activities_lead_id_created_at_idx` (composite) | A lead's timeline, newest first, straight out of the index with no sort step |
| `leads_name_trgm_idx`, `leads_company_trgm_idx` (GIN) | `ILIKE '%term%'` search, which a B-tree cannot accelerate |

There is deliberately **no** standalone index on `activities.lead_id` — the
composite index already covers lead_id lookups by the leftmost-prefix rule, so a
second one would only cost write throughput.

The trigram indexes need the `pg_trgm` extension. `schema.sql` wraps that in
exception handling: if the extension is unavailable the schema still applies
cleanly and search falls back to a sequential scan.

**Data integrity beyond types:** `CHECK` constraints reject blank names, malformed
emails, implausible phone numbers and over-long text. These duplicate the Go
validation on purpose — a bad migration or a manual `psql` INSERT should not be
able to create a nameless lead.

---

## Tests

```bash
cd backend
go test ./...                  # unit tests; DB tests skip automatically
go vet ./...                   # static analysis
gofmt -l .                     # formatting check (silence = clean)
```

**Unit tests** (`internal/model`) — 40+ cases across the validation rules,
normalisation, pagination arithmetic and the stage vocabulary. No database, no
HTTP server, runs in milliseconds.

**Integration tests** (`internal/store`) — 9 tests against real PostgreSQL,
covering the behaviour that *is* database behaviour: the `ON DELETE CASCADE`, the
unique index, and the transaction that keeps a stage change and its timeline entry
together. They skip unless `TEST_DATABASE_URL` is set:

```bash
createdb crm_leads_test
psql -d crm_leads_test -f ../db/schema.sql
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/crm_leads_test?sslmode=disable' \
  go test ./internal/store/ -v
```

Frontend:

```bash
cd frontend
npx tsc -b        # typecheck
npm run lint      # oxlint
npm run build     # production build
```

---

## Design decisions

**Stage changes write their own history.** `PATCH /leads/:id/stage` updates the
lead and inserts a `stage_change` activity **in one transaction**. The old value
comes from a CTE evaluated against the pre-update snapshot, so no extra read is
needed. Either both rows land or neither does — a lead's timeline can never
disagree with its stage. Re-selecting the current stage is a no-op and adds
nothing to the timeline.

**Two activity types are server-only.** `created` and `stage_change` are written
by the backend and **rejected** if a client submits them, so the timeline stays a
trustworthy record rather than something that can be forged.

**Any stage can follow any other.** Real sales work goes backwards ("they went
quiet, back to Contacted") and reopens closed deals. The lifecycle is presented as
the happy path in the UI, not enforced as a state machine — a CRM that cannot walk
a stage backwards forces people to lie to it.

**Filters live in the URL.** Shareable views, a working back button, and the
dashboard chart can link to `/leads?stage=negotiation` without the list page
knowing anything about the dashboard.

**One round trip per screen.** `GET /leads/:id` returns the lead with its
activities attached. The list gets its total from `COUNT(*) OVER()` — a window
function evaluated after `WHERE` but before `LIMIT` — so filtering, paging and
counting are one query, not two.

**Mutations patch state instead of refetching.** A stage change updates the one
row from the server's response: same data, no flicker, no scroll jump, no second
round trip. Creates and deletes *do* refetch, because they change which rows
belong on the page.

**Sort columns are allow-listed.** A sort column cannot be a bind parameter, so it
is interpolated into the SQL — which is safe only because the value is checked
against a fixed set first. `?sort_by=drop table leads` returns 422.

**Search wildcards are escaped.** `%` and `_` are special inside `ILIKE`.
Searching for `50%` finds leads containing "50%", not every lead in the database.

**422 vs 400.** 400 means "your JSON is broken"; 422 means "the server understood
you and refuses to store this". Splitting them lets a client tell a bug in its
serialisation apart from a user typing a bad email.

**Errors are translated once.** The store returns sentinel errors
(`ErrNotFound`, `ErrDuplicateEmail`); one function maps them to status codes. No
handler imports pgx, and no Postgres error code appears in the HTTP layer.

**Internal errors never leak.** A 500 logs the full wrapped error server-side and
returns a generic message — except in development, to save you a trip to the
terminal.

---

## Assumptions

1. **No authentication**, per the brief. All data is global; there is no notion of
   an owner, and activities record no author.
2. **Email is required and unique** (case-insensitively). A CRM with two records
   for the same person is worse than a rejected form, and it gives a natural key
   for de-duplication. Enforced by a functional unique index → 409.
3. **Company is required.** The brief lists it as a column in the main table. An
   individual with no company should be entered with their own name — supporting
   genuinely company-less leads would mean making the column nullable.
4. **Phone is optional**, and stored as `NULL` rather than `''` when absent, so
   "unknown" and "blank" are not two different states. Format is validated loosely
   (digits and `+ ( ) - . / space`) because international numbering is not
   worth being strict about.
5. **`PUT` replaces, it does not patch.** Every required field must be present;
   omitting `company` blanks it. That is the correct reading of PUT, and it is why
   the edit form loads the current values first. The one field that gets a partial
   endpoint is stage, because the inline dropdown needs it.
6. **Stage transitions are unrestricted** — see Design decisions.
7. **`notes` vs activities are different things.** `notes` is standing context
   (budget, org chart, quirks); activities are timestamped events. Notes are edited
   in place; activities are append-only.
8. **Deletes are hard deletes**, cascading to activities. A production CRM would
   soft-delete for recoverability and audit.
9. **Timestamps are `TIMESTAMPTZ`**, stored in UTC and rendered in the viewer's
   local timezone by the browser.
10. **`BIGSERIAL` ids, not UUIDs.** Small, sequential and easy to type while
    testing. A public multi-tenant API would want UUIDs so ids cannot be
    enumerated.
11. **Page size defaults to 10** so pagination is exercised by realistic data
    volumes rather than only appearing at a few hundred leads. Capped at 100.
12. **Conversion rate counts closed deals only** — `won / (won + lost)`. Including
    open leads in the denominator would make the number look worse every time a
    new lead arrived.

---

## What I would do with more time

**Correctness and testing**
- HTTP-level handler tests using `httptest`, covering each status code end to end
- Frontend component tests (Vitest + Testing Library) for the form, the stage
  picker's keyboard behaviour, and the URL-filter syncing
- A Playwright smoke test walking create → advance → log activity → delete
- Optimistic locking on `PUT` (`If-Match` on `updated_at`) so two people editing
  the same lead cannot silently overwrite each other

**Backend**
- Versioned migrations (golang-migrate or goose) instead of a single `schema.sql`.
  Fine for a fresh database; not fine once there is data to preserve
- Cursor (keyset) pagination for the list — `OFFSET` degrades on large tables
- A `/api/dashboard` cache (30–60s) — it is the most-hit and most expensive endpoint
- Structured request ids threaded through the logs for tracing
- Rate limiting, and a `RemoveExtraSlash`-style hardening pass
- `slog` output to a real aggregator, plus Prometheus metrics on latency and error rate

**Frontend**
- TanStack Query in place of the hand-rolled `useAsync` — proper caching,
  deduplication, background refetch and mutation rollback
- Optimistic stage changes with rollback on failure (currently it waits for the server)
- Virtualised rows, so thousands of leads scroll smoothly
- Undo on delete (soft delete + a toast action) rather than a confirm dialog
- Drag-and-drop Kanban board as a second view of the pipeline
- Bulk selection and bulk stage changes
- CSV import/export
- Full-text search across notes and activities, not just name and company

**Product**
- Owner assignment, follow-up reminders and overdue-lead highlighting
- Deal value, so the dashboard can show pipeline worth rather than only counts
- Stage-duration analytics: where do deals stall, and for how long
- Editable and deletable activities (currently append-only), with an audit trail
- Email/calendar integration to log activities automatically

**Types**
- Generate the TypeScript types from the Go structs (or an OpenAPI spec) instead of
  maintaining `frontend/src/lib/types.ts` by hand. It is the one place where the
  two halves can silently drift.
