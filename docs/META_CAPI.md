# Meta Conversions API and lead quality

How the CRM tells Meta which of its Facebook/Instagram lead-form leads became
qualified and converted (Meta's **Conversion Leads** integration), and how it
judges lead quality. Read this before switching it on.

- [What it does](#what-it-does)
- [What it does not do](#what-it-does-not-do)
- [Setup](#setup)
- [Lead quality](#lead-quality)
- [Lifecycle events](#lifecycle-events)
- [Security](#security)
- [API](#api)
- [Data model](#data-model)
- [Manual verification checklist](#manual-verification-checklist)
- [Still to do in Meta](#still-to-do-in-meta)

## What it does

1. A lead arrives from a **Meta lead form** (webhook, or Sync Now). The CRM
   stores Meta's lead id, page, form, campaign, ad set, ad and submit time.
2. The lead's **quality** is worked out from configurable rules, and worked out
   again when calls, meetings and other evidence are logged.
3. When the lead reaches a **mapped lifecycle stage** (created, qualified,
   converted), a conversion event is queued *in the same transaction as the
   stage change*.
4. A background sender delivers queued events to the Conversions API, with
   retries and backoff, and records every outcome in the delivery log.

## What it does not do

- **No browser Pixel or Google tag.** This repository has none; leads come from
  lead-form webhooks, not from a website. Each event has a stable `event_id`
  (`crm-<lead id>-<event name>-<live|test>`); a Pixel on a separate website
  could send the same name and id for Meta to deduplicate, but no such pairing
  exists to test here.
- **Only native Meta lead-form leads are sent.** Meta's Conversion Leads
  optimisation supports only Instant Forms; a lead without a Meta lead id has
  nothing to match, so manual, CSV and Google leads never send events.
- **Single tenant.** The CRM has no accounts or tenants, so there is no tenant
  isolation to enforce. All data belongs to one business.

## Setup

### 1. Server environment

In `backend/.env` (local) or the `.env` next to `docker-compose.yml`:

| Variable | What | Generate with |
|---|---|---|
| `ADMIN_API_KEY` | Guards every integration settings page and endpoint. 16+ characters. Empty turns admin actions off. | `openssl rand -hex 24` |
| `SECRETS_ENCRYPTION_KEY` | Encrypts tokens saved from the settings pages (AES-256-GCM). 32 bytes, base64 or hex. **Do not lose or change it**: tokens saved under an old key must be entered again. | `openssl rand -base64 32` |
| `META_GRAPH_VERSION` | Optional; pins the Graph API version, e.g. `v23.0`. Recommended. | |
| `PUBLIC_API_URL` | Where Meta reaches the lead webhook. | |

### 2. Database

`db/schema.sql` is idempotent. An **existing** database needs it applied once
(Docker only runs it on an empty volume):

```
psql "$DATABASE_URL" -f db/schema.sql
```

### 3. In Meta

| Where | What to do |
|---|---|
| Business settings > Users > System users | Create a system user, assign it the **dataset** (Pixel) with full control, and generate a token with `ads_management` (and `business_management` if asked). This is the **CAPI Access Token**. |
| Events Manager > Data sources | Note the **Pixel / Dataset ID**. Under **Test events**, copy the **test event code**. |
| Events Manager > your dataset > Settings | Set up the **CRM integration** / Conversion Leads for the dataset if Meta offers it, and pick the CRM stage to optimise for once events arrive. |
| developers.facebook.com > your app | The **App ID** (optional field) and, for lead retrieval, the lead webhook: see the Integrations page. |
| Business settings > Integrations > Leads access | Give the CRM's app access to the Page's leads. |

### 4. In the CRM

**Integrations > Meta CAPI settings** (`/integrations/meta-capi`), with the admin key:

1. Enter the Dataset ID, the access token and the test event code. Leave
   **Test mode** on. Tick **Send events to Meta**. **Save Configuration**.
2. **Test Connection**: a real call that checks the token can use the dataset.
   It sends no event.
3. **Send Test Event**: one event with the test code. It uses the newest real
   Meta lead id in the CRM (Meta rejects a made-up one), or a hashed example
   email if there is none yet.
4. When test events look right in Events Manager, turn **Test mode** off and
   save. This is refused until a connection test has passed.

## Lead quality

A score from 0 to 100, from rules an admin can change (`PUT /api/meta/quality-rules`).

| Default rule | Category | Points |
|---|---|---|
| Valid email address | Contact validity | 20 |
| Valid phone number (required for Good) | Contact validity | 20 |
| Said what service they need (form answer `service`) | Service fit | 15 |
| Gave a budget (form answer `budget`) | Budget fit | 15 |
| Responded to a follow-up (a logged call, email or meeting) | Follow-up response | 15 |
| Attended a meeting (meeting logged, or meeting/demo event done) | Meeting attendance | 15 |

- **Good** is 70 or more *and* every rule marked required has matched;
  **Average** is 40–69; **Poor** is under 40, or **confirmed invalid/spam** by a
  person.
- Each rule comes out **matched**, **failed** or **unknown**. Unknown means the
  data is not there yet, and it never counts against a lead. A lead with fewer
  than 40 points' worth of decidable rules is **Not Evaluated**, not Poor.
- Every lead keeps a sentence saying why (`quality_reason`), and the
  breakdown of every rule.
- The service and budget rules only check that the form asked. Change them to
  the answers that fit (`one_of`) and add penalties (negative points) for ones
  that do not.
- An admin can **override** the score, with a reason. A confirmed spam lead
  stays Poor whatever the override says.
- **Qualification status** is read from the stage (Qualified and later:
  qualified; Lost: not qualified; else pending), so the two never disagree.

Quality is internal. It is never sent to Meta.

## Lifecycle events

| Milestone | Default event name | When |
|---|---|---|
| Lead created | `Lead` | A Meta lead-form lead is created |
| Lead qualified | `Qualified Lead` | The lead enters Qualified (or a later stage) |
| Lead converted / won | `Converted` | The lead enters Won |

Each can be switched off and renamed. Other stages can be mapped too.

What is sent, per Meta's Conversion Leads [payload specification](https://developers.facebook.com/documentation/ads-commerce/conversions-api/conversion-leads-integration/payload-specification):

```json
{
  "event_name": "Qualified Lead",
  "event_time": 1792741869,
  "event_id": "crm-371-qualified-lead-live",
  "action_source": "system_generated",
  "user_data": { "lead_id": 869717083310123, "em": ["<sha256>"], "ph": ["<sha256>"] },
  "custom_data": { "event_source": "crm", "lead_event_source": "Pipeline CRM" }
}
```

- `lead_id` is Meta's own lead id, as a number. Email is trimmed and
  lower-cased, and the phone is reduced to digits with its country code. Both
  are SHA-256 hashed before they leave the server. Nothing else about the
  person is sent.
- `event_time` is when the lead entered the stage. Meta accepts up to 7 days
  back, and it must be after the lead was created. An older event goes with
  the send time.
- **Backfill:** Meta expects every stage, so a lead that jumps ahead (New to
  Won) also queues the earlier enabled stages, with their own times.
- **One milestone, one event:** the outbox has a unique key on (lead, event
  name, mode). A lead that leaves Qualified and comes back is not a second
  "Qualified Lead". A retry is the same row and the same `event_id`.
- **Consent:** a lead marked **Do not share with Meta** sends nothing, and its
  unsent events are cancelled. This is checked again at send time.
- **Spam:** a lead confirmed as spam never sends Qualified or Converted.
- **Retries:** timeouts, 5xx errors and rate limits are retried after 1 min,
  5 min, 30 min, 2 h and 6 h, honouring `Retry-After`, up to 6 attempts in all.
  A bad token (190), a missing permission (10, 200–299) or an event Meta
  refuses is **not** retried. It fails with Meta's code and message until a
  person fixes the cause and presses **Retry** (or **Retry all failed**).
- **Disabled:** nothing is sent and nothing new is queued. Events already
  queued wait as Pending.

Status of the integration: **Not configured**, **Not tested yet**,
**Connected**, **Invalid credentials**, **Permission denied**,
**Temporarily unavailable**, **Disabled**.

## Security

- **Admin only:** every `/api/integrations/meta-capi` route, the delivery log,
  sync, rules and overrides need `X-Admin-Key`. A wrong key gets 403 and is
  logged with the caller's address.
- **Secrets:** tokens are sealed with AES-256-GCM before they reach the
  database, and bound to their row. The API never returns them, only
  `••••••••1234`. They are never written to a log; the request log redacts
  `hub.verify_token`, `access_token` and similar parameters.
- **Browser:** the admin key is held in page memory only (not localStorage or
  sessionStorage), so a reload asks for it again.
- **Rotation:** **Update Access Token** replaces just the token, records when,
  and resets the status until it is tested.
- **Audit log:** settings saves, token rotations, tests, test events,
  disconnects, rule changes and overrides are recorded with the time and the
  caller's address. A token is recorded as "replaced", never by value.
- **Rate limits:** 60 requests a minute per caller on the integration routes,
  and 10 a minute on the two that call Meta (Test Connection, Send Test Event).

## API

All need `X-Admin-Key`.

| Method | Path | |
|---|---|---|
| GET | `/api/integrations/meta-capi` | Settings, masked token, status, event counts |
| PUT | `/api/integrations/meta-capi` | Save. `access_token` empty keeps the saved one. |
| PUT | `/api/integrations/meta-capi/token` | Update Access Token |
| POST | `/api/integrations/meta-capi/disable` | Disable Integration |
| POST | `/api/integrations/meta-capi/test` | Test Connection (no event) |
| POST | `/api/integrations/meta-capi/test-event` | Send Test Event (needs a test code) |
| GET | `/api/integrations/meta-capi/events?status=&lead_id=&from=&to=` | Delivery history |
| POST | `/api/integrations/meta-capi/events/:id/retry` | Retry one |
| POST | `/api/integrations/meta-capi/events/retry-failed` | Retry all failed |
| GET | `/api/integrations/meta-capi/audit` | Audit log |

These lead endpoints need no admin key: `PATCH /api/leads/:id/flags`
(`spam_confirmed`, `meta_share_opt_out`), `PATCH /api/leads/:id/qualification`,
`GET /api/meta/leads` (dashboard) and `GET /api/leads?quality=`. Overrides
(`PUT`/`DELETE /api/leads/:id/quality-override`) need the admin key.

## Data model

All of it is in `db/schema.sql`:

- `leads`: `quality`, `quality_score`, `quality_breakdown`, `quality_reason`,
  `quality_updated_at`, `quality_override*`, `qualification_reason`,
  `spam_confirmed`, `meta_share_opt_out`. `qualification_status` and the
  `meta_*` summary fields are worked out on read.
- `meta_lead_attribution`: one row per Meta lead.
- `capi_events`: the outbox and delivery log (`pending`, `retry_scheduled`,
  `sent`, `failed`, `cancelled`; `error_code`, `fbtrace_id`).
- `integration_settings`: config, sealed credentials, statuses.
- `app_settings`: the quality rules.
- `audit_log`.

## Manual verification checklist

Automated tests mock Meta. They prove the CRM's side only. Before relying on
it, check each of these against real Meta:

- [ ] Server has `ADMIN_API_KEY` and `SECRETS_ENCRYPTION_KEY`; `db/schema.sql` applied.
- [ ] Settings page: save Dataset ID, token, test code; the token shows only as `••••…`.
- [ ] **Test Connection** shows **Connected**. A deliberately wrong token shows **Invalid credentials**.
- [ ] **Send Test Event**, then **Events Manager > your dataset > Test events**: the event appears with the CRM's name, `action_source` system_generated, and Meta's match keys (lead id and/or email).
- [ ] Use Meta's **Lead Ads Testing Tool** to create a lead; it appears in the CRM with campaign/ad attribution and a quality score.
- [ ] Its **Lead** event appears under Test events within a minute (the sender runs every 30 s).
- [ ] Move it to **Qualified**, then **Won**: one event each under Test events; moving back and forth sends nothing more.
- [ ] Mark a lead **Do not share with Meta**: its unsent events show **Not sent** in the delivery history.
- [ ] **Events Manager > Diagnostics**: no errors about invalid `lead_id`, event time or parameters.
- [ ] Turn **Test mode** off; after the next stage change, the event appears in the dataset's **Overview** (not Test events).
- [ ] After a few days of live events, check the dataset's CRM / Conversion Leads setup shows the stages and their event counts.

## Still to do in Meta

These can't be done from the CRM:

- Create the system user and token with access to the dataset (above).
- Set up the dataset's CRM integration / Conversion Leads, choose the stage to
  optimise for, and create a **conversion leads** campaign optimising for it.
  Meta needs a volume of events at that stage before it can optimise.
- Keep the lead-form campaign on **Instant Forms**; other lead sources are not
  used by this optimisation.
- If you add a Pixel to your website and send the same lifecycle event from
  it, reuse the CRM's `event_name` and `event_id` so Meta deduplicates them.
