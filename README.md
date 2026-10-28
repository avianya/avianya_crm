# Pipeline — CRM Lead Management

Lead management and lifecycle tracking for a sales team. **React + TypeScript**
frontend, **Go** REST API, **PostgreSQL** database.

Email-and-password sign-in with roles (admin / manager / sales), per-user lead
ownership and visibility, and a set of optional integrations — Meta Lead Ads and
Google Ads lead capture, the Meta Conversions API, Google Sheets export and
reminder emails — that each stay switched off until you configure them.

> **New here?**
> - **Running it** is below — one Docker command, or a step-by-step local setup.
> - **[`docs/AUTH.md`](./docs/AUTH.md)** — logins, roles and how lead visibility works.
> - **[`docs/META_CAPI.md`](./docs/META_CAPI.md)** — Meta Conversions API and lead quality.
> - **[`docs/GOOGLE_SHEETS.md`](./docs/GOOGLE_SHEETS.md)** — the Google Sheets export.

---

## Contents

- [What it does](#what-it-does)
- [Tech stack](#tech-stack)
- [Running it](#running-it)
  - [Option A — Docker (one command)](#option-a--docker-one-command)
  - [Option B — Locally](#option-b--locally)
- [Environment variables](#environment-variables)
- [Project layout](#project-layout)
- [API reference](#api-reference)
- [Database schema](#database-schema)
- [Tests](#tests)
- [Design decisions](#design-decisions)
- [Assumptions](#assumptions)
- [What's next](#whats-next)
- [Logins, roles and lead visibility](docs/AUTH.md) (separate page)
- [Meta Conversions API and lead quality](docs/META_CAPI.md) (separate page)
- [Google Sheets export](docs/GOOGLE_SHEETS.md) (separate page)

---

## What it does

**Accounts, roles and team**
- Email-and-password sign-in; sign-ups wait for an admin's approval
- Three roles — **admin**, **manager**, **sales** — gating who can assign leads,
  manage the team and configure integrations
- Per-user lead ownership; a salesperson sees only the leads assigned to them,
  while managers and admins see every lead (see [`docs/AUTH.md`](./docs/AUTH.md))
- Forgot-password flow emails a one-hour reset link; admins can set or reset a
  member's password from the Team page

**Dashboard**
- Total leads, conversion rate, won/lost counts, leads added in the last 7 days
- Pipeline-by-stage bar chart — every bar is a link into the filtered list
- The most recently added leads

**Leads list**
- Table on desktop, cards on mobile
- Search by name or company (debounced, case-insensitive)
- Filter by stage (always-visible chips), source and owner
- Sort by date added, last updated, name, company or stage — in either direction
- Change a lead's stage inline, without leaving the list
- Select rows for **bulk** owner / stage / source changes or deletion
- **CSV import** (with a dry-run preview) and **CSV export** of the current view
- Paginated; every filter lives in the URL, so views are shareable and the back
  button works

**Add / edit lead**
- Full name, company, email, phone, source, stage, notes
- Validated on both sides; server-side field errors land on the right input
- A duplicate email returns 409 and is shown on the email field

**Lead detail**
- Full profile with clickable email and phone
- Step indicator for the lifecycle — click a step to move the lead there
- Owner assignment (managers and admins)
- Tasks and suggested next steps for the lead
- Send a templated email (preview with `{{fields}}` filled in, then send or
  just log it)
- Shareable **booking** and **feedback** links for the customer (see Public pages)
- Lead-quality score with manager overrides, plus spam / opt-out flags
- Activity timeline, newest first — log a note, call, email or meeting
  (Ctrl/Cmd+Enter to save); activities are editable and deletable
- Stage changes are written to the timeline **automatically**, in the same
  database transaction as the change itself

**Pipeline board, follow-ups, tasks and reports**
- A board view with one column per stage
- A follow-ups queue — who to contact and what is due, filterable by owner
- Tasks with due dates, rescheduling and completed / cancelled states
- Reports: weekly intake, source mix and time-in-stage trends
- Sales intelligence: insights and recommendations over the pipeline

**Integrations** (each optional, configured under Settings)
- **Meta Lead Ads** and **Google Ads** lead forms captured by webhook into the CRM
- **Meta Conversions API** — forwards lead events to Meta, with a per-event
  delivery log and one-click retries (see [`docs/META_CAPI.md`](./docs/META_CAPI.md))
- **Google Sheets export** over OAuth 2.0 — create a new sheet or pick an existing
  one, then sync (see [`docs/GOOGLE_SHEETS.md`](./docs/GOOGLE_SHEETS.md))
- Tokens for all of the above are **encrypted at rest** (AES-256-GCM)

**Automations, notifications and reminders**
- Admin-defined automation rules that run on events such as lead import
- In-app notification bell, plus optional reminder emails
- A per-member **daily digest** email (overdue and due follow-ups, stuck deals,
  leads going cold) that each member can turn off

**Public pages** (no login, reached by a signed token link)
- **Booking** — a customer picks a slot to meet
- **Feedback** — a rating, a comment and an optional referral

**Throughout**
- Light / dark / follow-the-system theme
- Skeleton loaders, designed empty states, toast confirmations, keyboard-accessible
  dialogs and menus

---

## Tech stack

| Layer | Choice | Why |
|---|---|---|
| Frontend | React 19 + TypeScript, Vite | Fast dev server; TS catches the null/undefined mistakes a nullable `phone` invites |
| Styling | Tailwind CSS v4 | Configured entirely from CSS; no config file to keep in sync |
| Routing | React Router 7 | Filter state in the URL means shareable, bookmarkable views |
| Charts | Hand-built with `div`s | A handful of bars did not justify a charting dependency, and this keeps the stage colours consistent with the rest of the app |
| Icons | lucide-react | Tree-shakeable |
| Backend | Go 1.26 + Gin | Small, explicit HTTP layer |
| DB driver | pgx/v5 | The best-maintained Postgres driver for Go; native support for the features this schema uses |
| Passwords | `golang.org/x/crypto` (bcrypt) | Standard, well-reviewed password hashing |
| SQL | Hand-written, no ORM | The whole app is filtering, aggregating and paginating — worth seeing the exact query that runs |
| Database | PostgreSQL 17 | Enums, partial/functional indexes, triggers, window functions |

**Runtime dependencies are deliberately few.** The backend has four direct
dependencies (Gin, pgx, godotenv, `x/crypto`); the frontend has three (React,
React Router, lucide-react). The Meta, Google and SMTP integrations are built on
the standard library's HTTP and `net/smtp` rather than vendor SDKs.

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

Integrations (Meta, Google, SMTP) stay off until you add their credentials — see
[Environment variables](#environment-variables).

---

### Option B — Locally

**Prerequisites**

| Tool | Version | Check |
|---|---|---|
| Go | 1.26+ | `go version` |
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

# Load the demo leads and activities (optional but recommended)
psql -U postgres -h localhost -d crm_leads -f db/seed.sql

# Optional: extra data for the sales-intelligence screens
psql -U postgres -h localhost -d crm_leads -f db/intelligence_seed.sql
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

Nobody can log in until the first admin exists, and the web app cannot create it
— so make it from the admin command-line tool (run from `backend/`; the password
is read from `CRM_PASSWORD` or asked for, never passed as a flag):

```bash
go run ./cmd/admin create-user -email you@yourcompany.com -name "Your Name" -role admin
```

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

## Environment variables

`backend/.env` — see [`backend/.env.example`](./backend/.env.example) for the
fully commented version. Everything except `DATABASE_URL` has a sensible default,
and every integration is off until its keys are filled in.

| Variable | Default | Purpose |
|---|---|---|
| `APP_ENV` | `development` | `development` = text logs + internal error details in responses. `production` = JSON logs, errors hidden from clients |
| `PORT` | `8080` | Port the API listens on |
| `DATABASE_URL` | — | **Required.** `postgres://user:pass@host:port/db?sslmode=disable` |
| `APP_TIMEZONE` | database setting | IANA zone for "today", "overdue" and report weeks |
| `APP_BASE_URL` | `http://localhost:5173` | Where the app is opened; used for links in reminder emails |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173,...` | Comma-separated browser origins allowed to call the API |
| `ADMIN_API_KEY` | — | Optional. A request with header `X-Admin-Key` acts as an admin (for scripts) |
| `SECRETS_ENCRYPTION_KEY` | — | 32 bytes base64. Encrypts integration tokens at rest; required for Meta CAPI and Google Sheets |
| `DIGEST_HOUR` | `8` | Hour (in `APP_TIMEZONE`) the per-member daily summary email goes out |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USERNAME` / `SMTP_PASSWORD` / `SMTP_FROM` | empty / `587` | Reminder emails. Leave `SMTP_HOST` empty and reminders stay in-app only |
| `PUBLIC_API_URL` | — | Public https address Meta/Google reach for lead webhooks |
| `META_APP_SECRET` / `META_VERIFY_TOKEN` / `META_PAGE_ACCESS_TOKEN` / `META_GRAPH_VERSION` | — | Meta Lead Ads webhook |
| `GOOGLE_LEADS_KEY` | — | Shared key for the Google Ads lead-form webhook |
| `GOOGLE_OAUTH_CLIENT_ID` / `GOOGLE_OAUTH_CLIENT_SECRET` / `GOOGLE_OAUTH_REDIRECT_URL` | — | Google Sheets export OAuth client |
| `GOOGLE_PICKER_API_KEY` / `GOOGLE_CLOUD_PROJECT_NUMBER` | — | Optional "choose from Drive" (Google Picker) |

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
| Meta / Google / Sheets "not set up" | Those integrations need their keys in `backend/.env` (and `SECRETS_ENCRYPTION_KEY`) — see above |

---

## Project layout

```
.
├── docker-compose.yml          Whole stack in one command
├── README.md                   This file
│
├── db/
│   ├── schema.sql              Tables, enums, indexes, constraints, triggers
│   ├── seed.sql                Demo leads and activities
│   └── intelligence_seed.sql   Extra data for the sales-intelligence screens
│
├── docs/
│   ├── AUTH.md                 Logins, roles and lead visibility
│   ├── META_CAPI.md            Meta Conversions API and lead quality
│   └── GOOGLE_SHEETS.md        Google Sheets export
│
├── backend/                    Go REST API
│   ├── cmd/server/main.go      Entry point: wiring + graceful shutdown
│   ├── cmd/admin/main.go       Admin CLI (create the first admin user)
│   ├── internal/
│   │   ├── config/             Environment loading
│   │   ├── model/              Domain types + validation rules  (+ tests)
│   │   ├── store/              All SQL lives here              (+ tests)
│   │   ├── api/                Routing, middleware, handlers
│   │   ├── auth/               Password hashing, sessions
│   │   ├── secrets/            AES-256-GCM sealing of integration tokens
│   │   ├── mail/               SMTP sending for reminders and digests
│   │   ├── meta/ · metaads/    Meta Lead Ads + Conversions API
│   │   ├── gsheets/            Google Sheets export
│   │   └── reminders/          Daily digest + follow-up reminders
│   ├── Dockerfile
│   └── .env.example
│
└── frontend/                   React + TypeScript
    ├── src/
    │   ├── lib/                api.ts (HTTP), types.ts, display.ts, templates.ts
    │   ├── hooks/              useAsync, useDebouncedValue, useTeam, ...
    │   ├── context/            Auth, Theme, Toasts (hooks split from providers
    │   │                       so Fast Refresh keeps working)
    │   ├── components/         UI primitives + feature components
    │   ├── pages/              Dashboard, Leads, Lead detail, Pipeline, Tasks,
    │   │                       Follow-ups, Reports, Automations, Integrations,
    │   │                       Meta CAPI/Leads, Team, Templates, Public pages
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

All `/api` routes sit behind an authentication middleware. The ways in that do
**not** need an existing session are sign-in, sign-up and password reset under
`/auth/*`, `GET /health`, and the token-based `/public/*` links; everything else
needs a logged-in session, and some endpoints are **admin-** or **manager-only**.

**Auth & account**

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/auth/login` · `/auth/logout` | Start / end a session (rate-limited) |
| `POST` | `/auth/signup` | Request an account (waits for admin approval) |
| `POST` | `/auth/forgot` · `/auth/reset` | Email a one-hour reset link, then use it |
| `GET` | `/auth/me` | The current user |
| `PUT` | `/auth/password` | Change your own password |

**Leads & activity**

| Method | Path | Purpose |
|---|---|---|
| `GET` / `POST` | `/leads` | List (filters, sort, pagination) / create |
| `GET` / `PUT` / `DELETE` | `/leads/:id` | One lead with its timeline / replace / delete |
| `PATCH` | `/leads/:id/stage` · `/owner` · `/notes` | Change stage / assign owner / edit notes |
| `GET` / `POST` | `/leads/export` · `/leads/import` · `/leads/bulk` | CSV export, CSV import (dry-run option), bulk action |
| `GET` / `POST` | `/leads/:id/activities` | Timeline / log a note·call·email·meeting |
| `PUT` / `DELETE` | `/activities/:id` | Edit / delete an activity |
| `GET` / `POST` | `/leads/:id/tasks`, `/tasks/:id` | Per-lead tasks; update, reschedule, complete, cancel |
| `POST` | `/leads/:id/emails` · `/email/preview` | Send a templated email / preview it |
| `GET` | `/leads/:id/links` · `/next-steps` | Booking/feedback links; suggested follow-ups |

**Pipeline, team & settings**

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/dashboard` · `/pipeline` · `/reports` · `/follow-ups` · `/sales-intelligence` | The read-only screens |
| `GET` | `/meta` | Stage / source / activity-type vocabularies with labels |
| `*` | `/team/:id/...` | Manage members, roles, passwords, test email, digest (**admin**) |
| `*` | `/email-templates`, `/automations`, `/lead-assignment` | Templates, automation rules, assignment (**manager/admin**) |
| `GET` / `POST` | `/notifications/...` | Reminder settings; mark read; retry email |

**Integrations** (**admin**)

| Method | Path | Purpose |
|---|---|---|
| `GET` / `POST` | `/webhooks/meta` · `/webhooks/google` | Inbound ad-lead webhooks |
| `*` | `/meta/settings` · `/meta/sync` · `/meta/leads` · `/meta/quality-rules` | Meta Lead Ads |
| `*` | `/integrations/meta-capi/...` | Meta CAPI: token, test event, delivery log, retries, audit |
| `*` | `/integrations/google-sheets/...` | Connect, pick/create a sheet, sync, retry |

**Public (token link, no login)**

| Method | Path | Purpose |
|---|---|---|
| `GET` / `POST` | `/public/booking/:token` | A customer picks a meeting slot |
| `GET` / `POST` | `/public/feedback/:token` | A rating, comment and optional referral |

**`GET /leads` query parameters**

| Parameter | Values | Default |
|---|---|---|
| `search` (or `q`) | matches name **or** company, case-insensitive | — |
| `stage` | `new` `contacted` `qualified` `proposal_sent` `negotiation` `won` `lost` \| `all` | `all` |
| `source` | `website` `referral` `linkedin` `cold_call` `other` \| `all` | `all` |
| `owner` | a member id \| `none` \| `all` | `all` |
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
| `401` | Not signed in |
| `403` | Signed in, but the role (or lead ownership) does not allow it |
| `404` | No such lead, or no such endpoint |
| `405` | Wrong HTTP method for that path |
| `409` | Email already belongs to another lead |
| `422` | Valid JSON, invalid data — `details` names each bad field |
| `429` | Rate limit hit (sign-in, password reset, public pages) |
| `500` | Server fault. Full error is logged; the client gets a generic message |
| `503` | `/health` only: the database is unreachable |

---

## Database schema

**23 tables, 3 enum types, 30 indexes, 2 triggers and 2 functions** — grouped by
area:

- **Core** — `leads`, `activities`, `tasks`, `lead_stage_history`, `deal_alerts`
- **People & access** — `team_members`, `sessions`, `password_resets`, `audit_log`
- **Reminders** — `notifications`, `daily_digests`
- **Email & automation** — `email_templates`, `automation_rules`, `automation_runs`
- **Ad leads & integrations** — `ad_leads`, `integration_settings`, `app_settings`,
  `meta_lead_attribution`, `capi_events`
- **Google Sheets** — `google_oauth_states`, `sheet_syncs`
- **Public pages** — `customer_links`, `lead_feedback`

**Three enum types** — `lead_stage`, `lead_source`, `activity_type` — so invalid
values are rejected by the database, not only by the API.

`lead_stage` is declared in **lifecycle order**, and a Postgres enum sorts by
declaration order rather than alphabetically. `ORDER BY stage` therefore means
"order by pipeline progress" for free, with no `sort_order` column to keep in
sync. `?sort_by=stage` uses exactly this.

Indexes are added for the queries the app actually runs: composite indexes that
serve a `WHERE` and its `ORDER BY` together, a functional unique index on
`lower(email)` (case-insensitive uniqueness → 409), and GIN trigram indexes for
`ILIKE '%term%'` search that a B-tree cannot accelerate. The trigram indexes need
the `pg_trgm` extension; `schema.sql` wraps that in exception handling, so if the
extension is unavailable the schema still applies cleanly and search falls back to
a sequential scan.

**Data integrity beyond types:** `CHECK` constraints reject blank names, malformed
emails, implausible phone numbers and over-long text. These duplicate the Go
validation on purpose — a bad migration or a manual `psql` INSERT should not be
able to create a nameless lead. A trigger keeps `updated_at` current.

---

## Tests

```bash
cd backend
go test ./...                  # unit tests; DB tests skip automatically
go vet ./...                   # static analysis
gofmt -l .                     # formatting check (silence = clean)
```

**Unit tests** (`internal/model`, and parts of `internal/api` and `internal/store`)
cover the validation rules, normalisation, pagination arithmetic, the stage
vocabulary, quality scoring and more — no database, no HTTP server, runs in
milliseconds.

**Integration tests** (`internal/store`) run against a real PostgreSQL and cover
the behaviour that *is* database behaviour: the `ON DELETE CASCADE`, the unique
index, and the transaction that keeps a stage change and its timeline entry
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

**Sessions live in the database.** Sign-in creates a row in `sessions` and sets an
HttpOnly, SameSite=Lax cookie holding a random token — only its SHA-256 hash is
stored. Sessions can be revoked server-side, time out on their own, and end the
moment a password or role changes; sign-ups wait for an admin to approve them
before any access is granted.

**Integration tokens are encrypted at rest.** Meta and Google credentials entered
on the settings pages are sealed with AES-256-GCM under `SECRETS_ENCRYPTION_KEY`
before they touch the database, and are never logged or sent to the browser.

**Every outbound integration call is logged and retryable.** Ad-lead webhooks,
CAPI events and sheet syncs each record their status, so a transient failure to
Meta or Google can be replayed from the UI rather than silently lost.

**Filters live in the URL.** Shareable views, a working back button, and the
dashboard chart can link to `/leads?stage=negotiation` without the list page
knowing anything about the dashboard.

**One round trip per screen.** `GET /leads/:id` returns the lead with its
activities attached. The list gets its total from `COUNT(*) OVER()` — a window
function evaluated after `WHERE` but before `LIMIT` — so filtering, paging and
counting are one query, not two.

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

1. **Authentication is email-and-password with three roles** (admin, manager,
   sales). Sign-ups wait for an admin's approval; sales users see only the leads
   assigned to them, while managers and admins see every lead. The exact
   visibility rules are in [`docs/AUTH.md`](./docs/AUTH.md).
2. **Email is required and unique** (case-insensitively). A CRM with two records
   for the same person is worse than a rejected form, and it gives a natural key
   for de-duplication. Enforced by a functional unique index → 409.
3. **Company is required.** An individual with no company should be entered with
   their own name — supporting genuinely company-less leads would mean making the
   column nullable.
4. **Phone is optional**, and stored as `NULL` rather than `''` when absent, so
   "unknown" and "blank" are not two different states. Format is validated loosely
   (digits and `+ ( ) - . / space`) because international numbering is not
   worth being strict about.
5. **`PUT` replaces, it does not patch.** Every required field must be present;
   omitting `company` blanks it. That is the correct reading of PUT, and it is why
   the edit form loads the current values first. Stage, owner and notes get their
   own `PATCH` endpoints because the inline controls need them.
6. **Stage transitions are unrestricted** — see Design decisions.
7. **`notes` vs activities are different things.** `notes` is standing context
   (budget, org chart, quirks); activities are timestamped events.
8. **Deletes are hard deletes**, cascading to activities. A production CRM might
   soft-delete for recoverability; the audit log retains a record of the action.
9. **Timestamps are `TIMESTAMPTZ`**, stored in UTC and rendered in the viewer's
   local timezone; "today" and "overdue" are computed in `APP_TIMEZONE`.
10. **`BIGSERIAL` ids, not UUIDs**, for internal rows — small and easy to type
    while testing. Public links (booking, feedback) use unguessable tokens so ids
    are never exposed.
11. **Page size defaults to 10** so pagination is exercised by realistic data
    volumes. Capped at 100.
12. **Conversion rate counts closed deals only** — `won / (won + lost)`. Including
    open leads in the denominator would make the number look worse every time a
    new lead arrived.
13. **Integrations are opt-in.** With no keys configured the app runs fully as a
    standalone CRM; nothing is ever pretended to be sent.

---

## What's next

**Correctness and testing**
- More HTTP-level handler tests using `httptest`, covering each status code end to end
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
- `slog` output to a real aggregator, plus Prometheus metrics on latency and error rate
- A background worker for retries and syncs instead of request-triggered replays

**Frontend**
- TanStack Query in place of the hand-rolled `useAsync` — proper caching,
  deduplication, background refetch and mutation rollback
- Optimistic stage changes with rollback on failure (currently it waits for the server)
- Virtualised rows, so thousands of leads scroll smoothly
- Undo on delete (soft delete + a toast action) rather than a confirm dialog
- Drag-and-drop on the pipeline board
- Full-text search across notes and activities, not just name and company

**Product**
- Deal value, so the dashboard can show pipeline worth rather than only counts
- Deeper stage-duration analytics: where do deals stall, and for how long
- Two-way calendar integration to log meetings automatically
- A mobile app, or an installable PWA, for reps in the field

**Types**
- Generate the TypeScript types from the Go structs (or an OpenAPI spec) instead of
  maintaining `frontend/src/lib/types.ts` by hand. It is the one place where the
  two halves can silently drift.
