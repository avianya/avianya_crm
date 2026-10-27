# Google Sheets export

Admins can export leads to a Google spreadsheet from **Integrations → Google
Sheets**. Each lead has one row, found by its **CRM Lead ID** in column A, so
syncing again updates rows instead of adding duplicates.

## What it can and cannot see

- **Permission requested:** `drive.file`, plus `openid` and `email` to show
  which account is connected. With `drive.file` the CRM can open only the
  spreadsheets it created, or that you chose with Google's file picker. It
  cannot see or change anything else in your Drive.
- **Who can use it:** admins only. Managers and salespeople do not see the
  card, and the API refuses them.
- **Whose leads are exported:** syncs run with the access of the admin who
  connected Google, which is every lead, filtered by the stages chosen in
  Configure. If that person stops being an active admin, syncing stops until
  an admin connects again.
- **What is exported:** only the columns ticked in Configure. Notes are off by
  default. Passwords, tokens and other internal data are never exported.
- **Credentials:** the client secret stays in `backend/.env`. The Google
  refresh token is stored in the database encrypted with
  `SECRETS_ENCRYPTION_KEY`. Access tokens are kept in memory for their hour
  and never stored. None of them are logged or sent to the browser. The one
  exception is that "Choose from Google Drive" passes a short-lived access
  token to the admin's page while the picker is open, which Google's picker
  requires.
- **Other people's data in the sheet:** the CRM writes only to an empty tab,
  or to a tab whose cell A1 is `CRM Lead ID` (one it wrote before). In that tab
  it changes only rows whose column A is a lead ID, and only its own columns.
  It adds new leads below the existing rows. Anything else, including your own
  columns to the right, is left alone. Rows for leads deleted in the CRM are
  left in the sheet.
- **Disconnect** revokes the CRM's access at Google, deletes the stored
  sign-in and turns automatic sync off. Rows already in the sheet stay.

## 1. Google Cloud Console setup

1. Open <https://console.cloud.google.com/> and pick a project, or create one
   (**Select a project → New project**).
2. **Turn on the APIs.** Go to **APIs & Services → Library** and enable:
   - **Google Sheets API**, to read and write the sheet.
   - **Google Drive API**, to list the spreadsheets the CRM may open.
   - **Google Picker API**, only if you want "Choose from Google Drive".
3. **Set up the OAuth consent screen.** Go to **APIs & Services → OAuth consent
   screen** (in newer consoles: **Google Auth Platform → Branding /
   Audience / Data access**).
   - **User type:** *Internal* if everyone is in your Google Workspace;
     otherwise *External*.
   - **App name, support email, developer contact:** your own details.
   - **Scopes / Data access:** add `.../auth/drive.file`, `openid` and
     `.../auth/userinfo.email`. `drive.file` is a non-sensitive scope, so no
     Google verification is needed for it.
   - **Test users** (External apps in *Testing* status): add the Google
     account(s) that will connect. While the app is in *Testing*, Google
     expires the sign-in after 7 days. **Publish the app** (Audience → Publish)
     to avoid that.
4. **Create the OAuth client.** Go to **APIs & Services → Credentials → Create
   credentials → OAuth client ID**.
   - **Application type:** *Web application*.
   - **Authorized JavaScript origins:** the address the CRM is opened at, e.g.
     `http://localhost:5173`. This is only needed for the picker.
   - **Authorized redirect URIs:** add exactly
     `http://localhost:8080/api/integrations/google-sheets/callback`
     for local testing. For production, use your API's public address, e.g.
     `https://crm.example.com/api/integrations/google-sheets/callback`.
     It must match `GOOGLE_OAUTH_REDIRECT_URL` character for character. The
     Configure card shows the value the server is using.
   - Click **Create**, then copy the **Client ID** and **Client secret**.
5. **Optional, for "Choose from Google Drive":**
   - **Credentials → Create credentials → API key.** Restrict it: under
     *Application restrictions* choose **Websites** and add your CRM's
     address (e.g. `http://localhost:5173/*`). Under *API restrictions*, allow
     only **Google Picker API**.
   - The **project number** is on the project's **Dashboard** / **Settings**
     page. It is the numeric one, not the project ID.

## 2. backend/.env

```env
# Required for Google Sheets
GOOGLE_OAUTH_CLIENT_ID=1234567890-abc.apps.googleusercontent.com
GOOGLE_OAUTH_CLIENT_SECRET=GOCSPX-...
GOOGLE_OAUTH_REDIRECT_URL=http://localhost:8080/api/integrations/google-sheets/callback
# Already needed for integrations; 32 random bytes, e.g. openssl rand -base64 32
SECRETS_ENCRYPTION_KEY=...

# Optional: "Choose from Google Drive"
GOOGLE_PICKER_API_KEY=AIza...
GOOGLE_CLOUD_PROJECT_NUMBER=123456789012
```

`backend/.env` is git-ignored. Do not put these values in
`backend/.env.example` or in frontend code. Restart the backend after
changing them.

## 3. Local testing

1. Run the schema once to create the new tables (it is safe to re-run):
   `psql "$DATABASE_URL" -f db/schema.sql`
2. Start the backend and frontend. Open **http://localhost:5173**, log in as
   an **admin**, and go to **Integrations**. Use `localhost` for both the app
   and the API, so the login cookie reaches the callback.
3. **Connect Google account.** Google's consent screen opens. Allow access.
   You return to Integrations with "Google account connected".
   - If you press **Cancel** on Google's screen, the page says nothing was
     connected.
   - If you untick the Drive permission, the page says why and connects
     nothing.
4. **Configure.** Click **Create new spreadsheet** (or **Choose from Google
   Drive**), pick a tab or name a new one, then arrange the columns, choose
   which stages to export and whether to sync automatically. Click **Save**.
5. **Test connection.** It should say the CRM can write to the tab. If you
   choose a tab that has other data in it, the test explains that the CRM
   will not write there.
6. **Sync now.** The history shows it running, then *Done* with "N added".
   Open the sheet: there is a header row, then one row per lead.
7. Change a lead in the CRM (e.g. its name or stage) and **Sync now** again.
   The history shows "0 added · N updated", and the sheet still has one row
   per lead.
8. Turn on **automatic sync** (every 5 minutes is quickest to test). Add a
   lead, and within the interval it appears in the sheet. A run with nothing
   new writes nothing and adds no history entry.
9. **Failures and retry.** A failed or partly-done sync shows its error with a
   **Retry** button. To try it, rename the CRM's tab in Google Sheets, then
   Sync. It fails with a "not a tab" or "cannot open" message. Rename the tab
   back, then click Retry.
10. **Permissions.** Log in as a salesperson. There is no Integrations page, and
    `GET /api/integrations/google-sheets` answers 403.
11. **Disconnect.** Google's "Third-party apps with account access" page
    (<https://myaccount.google.com/permissions>) no longer lists the CRM.
    Sync is refused until you connect again.

## 4. Production

- Serve the app and API over **HTTPS**, on the same site (e.g.
  `https://crm.example.com` with the API under `/api`), so the session cookie
  reaches the callback.
- Set `GOOGLE_OAUTH_REDIRECT_URL` to the public callback address, add the same
  address to the OAuth client's **Authorized redirect URIs**, and add the
  app's origin to **Authorized JavaScript origins** if you use the picker.
  Set `APP_BASE_URL` to the app's address, because the callback sends people
  back there.
- **Publish** the OAuth consent screen, so sign-ins do not expire after 7 days.
- Keep `SECRETS_ENCRYPTION_KEY` stable. If it changes, the stored sign-in
  cannot be read, and an admin must disconnect and connect again.
- Automatic sync runs inside the backend process, every 5 to 60 minutes as
  configured. Syncs run one at a time, even across several backend instances.
  They use a database lock for this.
- If Google rate-limits the CRM (HTTP 429) or has a temporary error (5xx), the
  CRM retries up to 5 times with growing waits, and honours `Retry-After`.

## 5. API (admin only)

| Method | Path | What it does |
|---|---|---|
| GET | `/api/integrations/google-sheets` | Status, settings, last 20 syncs |
| PUT | `/api/integrations/google-sheets` | Save spreadsheet, tab, columns, stages, auto sync |
| POST | `/api/integrations/google-sheets/connect` | `{url}` of Google's consent page |
| GET | `/api/integrations/google-sheets/callback` | Google's redirect back; checks the session and state |
| POST | `/api/integrations/google-sheets/test` | Checks access without writing |
| GET | `/api/integrations/google-sheets/spreadsheets` | Spreadsheets the CRM may open |
| GET | `/api/integrations/google-sheets/spreadsheets/:id` | A spreadsheet's tabs |
| POST | `/api/integrations/google-sheets/spreadsheets` | Create one `{name}` |
| GET | `/api/integrations/google-sheets/picker-token` | Short-lived token for the picker |
| POST | `/api/integrations/google-sheets/sync` | Start a sync (runs in the background) |
| POST | `/api/integrations/google-sheets/syncs/:id/retry` | Run a failed or partial sync again |
| POST | `/api/integrations/google-sheets/disconnect` | Revoke and forget the sign-in |
