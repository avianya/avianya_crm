# Logins, roles and lead visibility

Everyone logs in with their own email and password. What they see depends on
their role:

| Role | Leads they see | Can also |
|---|---|---|
| **Admin** | Every lead | Everything: add people, set roles and passwords, switch logins off, automations, integrations and Meta CAPI settings |
| **Manager** | Every lead | Assign and reassign leads, delete leads, import CSV, override quality scores, edit email templates |
| **Sales** | **Only leads assigned to them** | Work those leads: edit them, change stage, log calls and notes, plan events, send emails. Leads they add are assigned to them. |

The rule is applied everywhere: the lead list, search, filters, pagination,
CSV export, lead details, the timeline, events and tasks, the calendar,
follow-ups, the dashboard, the pipeline, reports, Intelligence, reminders, and
Meta Leads. A salesperson who opens a link to someone else's lead gets "not
found", exactly as for a lead that does not exist.

## How it works

- **Accounts are team members.** A team member (someone leads can be assigned
  to) becomes an account once they have a role and a password. "Assigned to"
  is the existing lead owner (`leads.owner_id`); no lead data was moved or
  changed. Existing members start as **Sales with no password**, so nobody can
  log in until an admin gives them one.
- **Passwords** are stored as bcrypt hashes and never returned by the API.
- **Sessions:** a random token in an **HttpOnly, SameSite=Lax** cookie (scoped
  to `/api`, and Secure outside development). Only the token's SHA-256 hash is
  stored. A session ends after 7 days, after 12 hours unused, at logout, when
  the person's password is changed or reset, or when their login or role is
  changed.
- **The user is never taken from the request.** The server works out who is
  asking from the session cookie only. A salesperson who sends someone else's
  `owner_id` still gets the lead assigned to themselves.
- **Lead scoping happens in the database layer.** For a Sales viewer, every
  read of leads runs with `WITH leads AS (SELECT * FROM leads WHERE owner_id =
  <them>)` in front of it, so one rule covers every query (see
  `backend/internal/store/scope.go`). Changes to a lead first check that the
  lead is visible.
- **CSRF:** every change must carry an `X-Requested-With` header, which
  another site cannot send. The frontend sends it.
- **Brute force:** at most 10 login attempts a minute from one address. A
  wrong password and an unknown email get the same answer after the same wait.
- **The last admin** cannot be demoted, switched off or deleted.
- **Audit log:** password resets, role changes and account deletions are
  recorded with who did them.
- `ADMIN_API_KEY` (optional) lets a **script** act as an admin with the
  `X-Admin-Key` header. People don't use it; leave it empty if no script needs
  it.

## 1. Update the database

This adds the account columns and the `sessions` table. It changes no leads
and is safe to run more than once:

```
psql "$DATABASE_URL" -f db/schema.sql
```

## 2. Create the first admin

Nobody can log in yet, so the first admin is made from the command line. From
`backend/` (it reads `DATABASE_URL` from `backend/.env`):

```powershell
go run ./cmd/admin create-user -email you@yourcompany.com -name "Your Name" -role admin
```

It asks for the password (10 characters or more). Make sure nobody is watching
the screen, because it shows as you type. To avoid typing it at the prompt,
put it in an environment variable for that one command:

```powershell
$env:CRM_PASSWORD = "your long password here"; go run ./cmd/admin create-user -email you@yourcompany.com -name "Your Name" -role admin; Remove-Item Env:CRM_PASSWORD
```

If the email belongs to an existing team member, that member becomes an admin
and keeps their leads. To reset a forgotten password from the command line:

```powershell
go run ./cmd/admin set-password -email you@yourcompany.com
```

Restart the backend, open the app, and log in.

## 3. Test with two salespeople

1. Log in as the admin and open **Team**.
2. **Add member** twice, for example *Asha* (`asha@yourcompany.com`) and
   *Ravi* (`ravi@yourcompany.com`).
3. For each one: set **Role** to **Sales**, click **Give login**, and set a
   password.
4. Open **Leads**. Assign some leads to Asha and some to Ravi, from a lead's
   **Assigned to** field or by ticking several and using **Assign to**. Leave
   one unassigned.
5. **Log out** (top right). Log in as Asha, in another browser or a private
   window so both can stay logged in. Check that:
   - **Leads** lists only Asha's leads, and search, filters, pages and
     **Export CSV** only ever show hers.
   - The **Dashboard**, **Pipeline**, **Reports**, **Follow-ups**, **Tasks**,
     the **calendar** and **Meta Leads** only count her leads.
   - Opening one of Ravi's leads by its URL (`/leads/<id>`) shows *not found*.
   - She cannot reassign or delete leads, and Integrations, Automations and
     the admin pages are not in her menu.
   - A lead she adds is assigned to her.
6. Log in as Ravi: he sees only his.
7. As the admin, reassign one of Asha's leads to Ravi. It disappears from
   Asha's list and appears in Ravi's.
8. As the admin, **Switch off** Ravi. He is logged out at once and cannot log
   in again until he's switched back on.

## Notes

- Open the frontend and the API on the **same host name**: both on
  `localhost`, or both on the same domain. `http://localhost:5173` with an API
  at `http://127.0.0.1:8080` counts as a different site, and the browser will
  not send the login cookie.
- In production, serve over **HTTPS**. Outside `APP_ENV=development` the
  cookie is marked Secure (browsers still accept it on `http://localhost`).
- Unassigned leads are visible to admins and managers only, so assign them.
