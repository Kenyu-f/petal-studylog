# StudyLog — explanation.md

This document explains how StudyLog works internally: not just what the
features are, but *why* it is built this way, so another developer can
pick up the codebase without reverse-engineering it.

---

## 1. Project Overview

StudyLog is a two-person study log and goal-tracking app. It is not a
generic habit tracker: every day's study time is meant to be traceable
back to a goal, and every goal is meant to be traceable back up to a
long-term ambition, via an arbitrary-depth **Goal Tree**:

```
Long-term Goal -> Medium-term Goal -> Short-term Goal -> Study Session
```

The central UI is **Grass**, a GitHub-contribution-style heatmap where
each day's cell shows how close the user got to that day's target for a
chosen goal. Two people ("Ken" and "Hasti" in the examples, but any two
usernames) share one **Study Group**: they see each other's logs side by
side, but the product deliberately has **no ranking, leaderboard, or
"who studied more" comparison** — see section 7 (User/Group Model) and
the Decisions log at the end.

## 2. Architecture

StudyLog is a classic server-rendered web app, not a single-page app:

```
Browser
  |  (HTML pages, cookie-based session)
  v
Go HTTP server (net/http, stdlib ServeMux)
  +- Page handlers  -> html/template -> full HTML response
  +- JSON API       -> used by small vanilla-JS "islands" for the
  |                     interactive parts (Grass grid, dialogs)
  +- store.Store    -> in-process, file-backed data layer
                          |
                          v
                    data/studylog.json  (single JSON document on disk)
```

There is no build step, no bundler, no framework runtime. The server
renders the page shell (nav, goal tree, initial state); a small amount of
vanilla JavaScript then talks to a JSON API for anything that needs to
update without a full page reload (adding a session, switching the Grass
goal/year, editing a goal). This hybrid is intentional: most of the app
(auth, goal CRUD, page layout) is simple enough that full page reloads are
completely fine and easier to reason about; the Grass grid and the
"click a cell -> add a session" flow specifically benefit from not
reloading the page, so those are the only parts that use `fetch()`.

## 3. Technology Stack

| Layer | Choice | Why |
|---|---|---|
| Language | Go (stdlib only: `net/http`, `html/template`, `encoding/json`) | Single static binary, no package manager step to run it, easy for another developer to read top-to-bottom. |
| Routing | Go 1.22's enhanced `http.ServeMux` (`"GET /api/goals/{id}"` patterns) | As of Go 1.22 the stdlib router supports method + path-parameter patterns, so a third-party router (chi, gorilla/mux, gin) is unnecessary. |
| Templates | `html/template` | Auto-escapes user input (important: Descriptions are freeform user text), no separate templating dependency. |
| Persistence | A hand-written JSON-file store (`internal/store`) | See Decision D1 below — this replaces the originally-requested SQLite for this build, with a swap-friendly interface. |
| Frontend | Vanilla HTML/CSS/JS, no framework, no build step | Requested explicitly; also genuinely sufficient for the amount of interactivity here (one grid, two dialogs). |
| Auth | Username/password with a stdlib-only salted+stretched SHA-256 hash and a random-token cookie | No external crypto dependency; see section 18. |

## 4. Directory Structure

```
studylog/
+- main.go                     entry point: opens the store, parses
|                                templates, starts the HTTP server
+- go.mod                      module studylog (zero external deps)
+- internal/
|  +- models/models.go        plain data structs (User, Group, Goal,
|  |                            StudySession) - no behavior
|  +- store/store.go          the persistence layer (JSON file, mutex-
|  |                            guarded, write-through)
|  +- authutil/authutil.go    password hashing + session tokens
|  +- grass/grass.go          pure functions: Grass cell math, Goal
|  |                            progress math, goal-tree flattening
|  +- handlers/handlers.go    HTTP routing, request parsing, page
|                                rendering, JSON API
+- templates/                  html/template files, one per page
|  +- nav.html                 shared top navigation partial
|  +- login.html, setup.html   auth pages
|  +- dashboard.html           the Grass page
|  +- goals.html               the Goal Tree page
+- static/
|  +- css/style.css            the entire stylesheet
|  +- js/
|     +- app.js                Grass rendering + day dialog
|     +- goals.js              goal create/edit/delete dialog
+- data/                       the JSON "database" file lives here
|  +- .gitkeep
+- explanation.md               this document
```

This mirrors the architecture directly: one package per concern
(`models` = shape, `store` = persistence, `grass` = calculation,
`handlers` = HTTP), each importable and testable independently.

## 5. Data Model

Four entities, all in `internal/models/models.go`:

- **User** — id, username, display name, password hash+salt, `groupId`.
- **Group** — id, name. The unit of sharing: two Users share one Group.
- **Goal** — a node in the Goal Tree (see section 7).
- **StudySession** — one logged block of study time (see section 8).

**Decision D2 — targets are minutes-only in the MVP.** The spec's
examples mix units ("60 min/day", "20 problems/day", "5 papers/week").
Rather than build a generic unit-conversion system now, `Goal.TargetValue`
/`TargetUnit`/`TargetPeriod` are stored generically, but the *calculation*
code (`internal/grass`) only ever compares `StudySession.DurationMin`
against `Goal.TargetValue` — i.e. only minute-based targets are
functionally measurable right now. `TargetUnit` is still stored and
displayed as free text, so a goal can be labeled "20 problems/day" for
humans to read, but its Grass/progress math would need a `problems` count
on StudySession (see section 24, Future Extensions) to be truly
measurable. This was called out rather than silently building a fake
generic unit system that isn't actually connected to real logging yet.

## 6. User / Group Model

Every User belongs to exactly one Group (`User.GroupID`). Every Goal and
StudySession also carries a `GroupID`, so **all data access is scoped by
group**: handlers always filter `store.*InGroup(user.GroupID)` rather than
returning everything in the file. This is what makes "two people share a
workspace, others don't see it" work without a separate permissions
system — it's a simple `WHERE group_id = ?` filter, done in Go instead of
SQL.

A Goal can be:
- **Personal** — `OwnerID` set, `Shared = false`. Only labeled as "mine"
  but still visible to the partner (read-only) since the whole point is
  that Study Logs are visible to both people (spec section 8).
- **Shared** — `Shared = true`, `OwnerID = ""`. Conceptually owned by the
  Group itself (e.g. a joint goal like "Read 10 ML papers together").

**No ranking, no leaderboard.** The spec is explicit that this must not
become a competition. Concretely: the "Today" summary on the dashboard
renders each person's card in a plain grid, unordered by value (not
sorted by minutes), with no medal/crown/percentage-ahead UI — just each
person's number, side by side. There is intentionally no combined "team
total minutes" leaderboard screen.

## 7. Goal Tree

`Goal.ParentGoalID == ""` marks a root goal; any other goal points to its
parent by ID. There is **no fixed depth** — `internal/grass.SortGoalsForTree`
walks the tree recursively and works for one level or ten.

`Goal.Type` (`long-term` / `medium-term` / `short-term`) is stored as a
free-form label for *display* only (e.g. shown as a small tag in the
Goals page). It does not constrain where in the tree a goal can sit —
that's controlled purely by `ParentGoalID`, per the spec's explicit
request that goals not be forced into a fixed 3-level structure.

Deleting a goal that still has children is rejected (HTTP 409) rather
than silently cascading — the UI tells the user to move or delete the
children first. This is a deliberate MVP simplification: cascading
delete of a goal subtree (and all its logged sessions) is destructive
and easy to get wrong, so it's out of scope until there's a real need
for it.

## 8. Study Session Model

A StudySession is: id, userId, groupId, date (`YYYY-MM-DD`), durationMin,
subject, goalId (optional), description (optional).

**Validation rule** (`handlers.apiCreateSession`): a session must have
either `durationMin > 0` **or** a non-empty `description`. This directly
implements the spec's UX requirement that time-only and description-only
entries both be valid, while still rejecting a totally empty submission.
Multiple sessions per day are just multiple rows with the same `date`;
nothing about the model assumes "one session per day".

## 9. Grass Calculation

For a chosen Goal and a date range, `internal/grass.BuildCells`:

1. Sums `DurationMin` of every StudySession on that date whose `GoalID`
   equals the chosen goal's ID (exact match — see the "why not
   aggregate descendants" note below).
2. Computes `ratio = actual / goal.TargetValue` (0 if `TargetValue` is 0,
   to avoid a division-by-zero producing NaN in the JSON).
3. Computes `intensity = clamp(ratio, 0, 1)` — this is the number that
   drives the cell color.

Worked example from the spec:

```
actual = 45 minutes
target = 60 minutes
ratio  = 45 / 60 = 0.75    -> shown to the user as "75%"
intensity = min(0.75, 1) = 0.75   -> this is what picks the color
```

And for an over-achieved day:

```
actual = 90, target = 60
ratio = 1.5    -> still shown as "150%" in the tooltip
intensity = min(1.5, 1) = 1   -> color is capped at the strongest pink
```

**Why exact goal match, not "sum descendants too"?** Section 3 of the
spec says an abstract top-level goal (like "Get into MIT") should let the
user pick one of its *measurable descendant* goals to view as Grass,
rather than the app inventing an aggregate metric for the abstract goal
itself. The dashboard's Grass goal-selector therefore only lists goals
with `ProgressMode == "rate"` (i.e. goals that actually have a daily
target) — abstract parents never appear in that dropdown, and the numbers
per goal stay exact rather than double-counted up a tree that mixes
units. (Contrast this with cumulative Goal *progress*, section 11, which
deliberately does aggregate descendants — a different, separately
justified calculation. See Decision D3.)

## 10. Grass Color Normalization

`intensity` (always 0-1, see above) is mapped to a color client-side in
`static/js/app.js` (`pinkForIntensity`). It's a piecewise-linear
interpolation between five stops:

| intensity | color |
|---|---|
| 0 | white `#ffffff` |
| 0.25 | very light pink |
| 0.5 | light pink |
| 0.75 | medium pink |
| 1.0 (and anything >= 1.0, since intensity is pre-clamped) | strongest pink |

This is done in JS rather than Go so the color scale is easy to
eyeball/tweak by editing one small function without recompiling the
server, and because color is a presentation concern, not a data concern —
the API returns `intensity` as a plain number, and any future client
(e.g. a native app) could pick its own palette from the same number.

## 11. Goal Progress Calculation

`internal/grass.ComputeProgress` implements section 7's requirement
carefully: **do not invent a percentage for goals with no well-defined
measure.** Every Goal has a `ProgressMode`:

- **`none`** — no numeric progress at all. Used for goals like "Get into
  MIT" that have no simple sum-based definition of "72% done". The UI
  renders these as plain tree nodes with no bar.
- **`rate`** — a recurring per-day (or per-week) target. This is what
  drives Grass (section 9), not a lifetime progress bar — there's no
  single "% done" for something recurring forever.
- **`cumulative`** — a one-time total target (e.g. "300 minutes on this
  problem set"). Progress = `actual / TargetValue`, and `actual` is
  summed **recursively down the subtree**: this goal's own
  directly-linked session minutes, plus the same recursive sum for every
  child goal. This implements the spec's explicit request that a parent
  goal's progress be computable from its descendants' logged work.

This is why "Mathematics — 60 min/day" (a `rate` goal) never shows a
progress bar, while a child like "Calculus problem set — 300 min total"
(a `cumulative` goal) does. Mixing the two concepts into one number would
produce exactly the meaningless-percentage problem the spec warns
against.

## 12. Date Handling

Dates are stored and transmitted as plain `"YYYY-MM-DD"` strings
everywhere — in `StudySession.Date`, in Grass API query params
(`start`/`end`), and in the JSON cell objects. There is deliberately no
timezone-aware `time.Time` for the *date a session belongs to*: a study
session is something the user logged "for today", not a precise instant,
so treating it as a timezone-naive calendar date avoids a whole class of
"it's 11pm so my session got logged as tomorrow" bugs. `CreatedAt`/
`UpdatedAt` audit fields, by contrast, are real UTC `time.Time` values,
since those *are* meant to be precise instants.

## 13. Database Schema

There is no SQL schema — see Decision D1 — but the JSON file has an
equivalent logical shape: the four model structs from section 5
collected into one document:

```json
{
  "users":    [ {"...User fields..."} ],
  "groups":   [ {"...Group fields..."} ],
  "goals":    [ {"...Goal fields..."} ],
  "sessions": [ {"...StudySession fields..."} ],
  "tokens":   { "<session-token>": "<userId>" }
}
```

If/when this becomes a real SQL database (see Future Extensions), the
tables map 1:1 onto these four arrays, with `tokens` becoming a real
`sessions` table with an expiry column instead of living forever in the
JSON blob.

## 14. API / Server Flow

All routes are registered in `handlers.App.Routes()` using Go 1.22
pattern syntax, e.g.:

```go
mux.HandleFunc("GET /api/grass", a.withAuthJSON(a.apiGrass))
mux.HandleFunc("PUT /api/goals/{id}", a.withAuthJSON(a.apiUpdateGoal))
```

Two small middleware wrappers handle auth:
- `withAuth` — for full HTML pages; redirects to `/setup` (no group yet)
  or `/login` (no valid session cookie) instead of erroring.
- `withAuthJSON` — for the JSON API; returns `401 {"error": "..."}`
  instead of redirecting, since a `fetch()` call can't usefully follow an
  HTML redirect.

A typical write request (e.g. `POST /api/sessions`):

```
JS fetch() -> handlers.apiCreateSession
  -> decode + validate JSON body
  -> verify referenced Goal belongs to the caller's Group
  -> build a models.StudySession, store.CreateSession()
       -> store acquires its mutex, appends, writes JSON to disk
          (temp file + rename, so a crash mid-write can't corrupt data)
  -> 201 Created with the saved session as JSON
```

## 15. Frontend Rendering Flow

The dashboard (`templates/dashboard.html`) is rendered server-side with
the list of the user's "rate" goals (for the `<select>`) and today's
date, but the Grass grid itself is empty HTML ("Loading...") until
`static/js/app.js` runs:

```
script runs at end of <body>
  -> populate year <select>
  -> GET /api/goals              (full goal list, used for dropdowns)
  -> GET /api/grass?goalId=...&start=...&end=...
  -> build the GitHub-style week/weekday grid in JS, color each cell
  -> GET /api/day?date=today     (for the "Today" summary cards)
```

Every subsequent interaction (switching the goal or year, clicking a
cell, saving/deleting a session) re-runs just the relevant `fetch()` +
re-render step — never a full page reload. The Goals page, by contrast,
reloads the page (`window.location.reload()`) after any goal
create/edit/delete: goal edits are infrequent enough that the simplicity
of "just reload" outweighs the benefit of a live-patched tree view.

## 16. Cell Click Interaction

Clicking a Grass cell (`<button class="grass-cell" data-date="...">`)
calls `openDayDialog(date)` in `app.js`:

1. Opens a native `<dialog>` element (no modal library needed — HTML5
   `<dialog>` handles focus trapping, `Esc` to close, and the backdrop).
2. `GET /api/day?date=YYYY-MM-DD` fetches every session either user
   logged that day, each annotated with the display name and goal title.
3. Existing sessions are listed (with a "Delete" button, only shown on
   the viewer's own sessions — enforced again server-side in
   `apiDeleteSession`, not just hidden in the UI).
4. A short "Add a session" form underneath lets the user log a new
   session for that date without leaving the dialog. On save, the
   dialog's session list *and* the underlying Grass cell both refresh
   (`loadGrass()` and `loadTodaySummary()` run again), so the color
   update is visible immediately behind the dialog.

Past dates work identically to today's — the dialog doesn't care whether
`date` is in the past, present, or future, matching the spec's
requirement that past cells also be clickable to add or edit records.

## 17. Study Description Handling

`Description` is a plain `<textarea>`, always optional both in the UI
(no `required` attribute, no asterisk) and in the API validation
(`apiCreateSession` only requires *either* duration>0 *or* a non-empty
description, never both). It is rendered back to the user through
`html/template` (server-rendered pages) or via `textContent`/`escapeHTML`
(client-rendered dialog content) — both of which HTML-escape it
automatically, so a description like `<script>` typed by the user is
displayed as literal text, not executed. This matters more than it might
seem: Description is explicitly meant to be free-form ("微分の演習問題を
20問解いた"), so it's the field in the app most likely to contain
characters that would otherwise be dangerous to render unescaped.

## 18. Authentication

MVP auth (`internal/authutil`) is intentionally minimal:

- Passwords are hashed as `SHA-256(salt + ":" + password)`, then the
  digest is re-hashed through SHA-256 **100,000 times** as a manual
  stretch (a simplified stand-in for PBKDF2/bcrypt/argon2), with a random
  16-byte salt per user.
- Login issues a random 32-byte token stored in an `httpOnly`,
  `SameSite=Lax` cookie; the token maps to a user ID in the store's
  `tokens` map. Logout deletes that map entry.
- There is no password-reset flow, no rate limiting on login attempts,
  and no email verification.

**Why not bcrypt?** `golang.org/x/crypto/bcrypt` is the standard real
answer, but it's an external module, and this build environment does not
have reliable access to the Go module proxy (see Decision D1). Manual
SHA-256 stretching with a per-user salt is a reasonable stopgap for a
private two-person tool, but is explicitly **not** what should be used if
this app is ever exposed to the public internet with untrusted signups —
see Future Extensions.

## 18b. Password Recovery (no email)

Added after the initial MVP, at the user's request. StudyLog has no SMTP
integration (adding one would pull in real external-service configuration
for a two-person private tool — against the "minimal dependencies"
priority), so "forgot password" is solved with a **recovery code**
instead of an email link:

- At account creation (`/setup`) and again after every successful reset,
  the server generates a random 12-character code (grouped as
  `XXXX-XXXX-XXXX`, from an alphabet that excludes visually-ambiguous
  characters like `0`/`O`/`1`/`I`) and shows it to the user **exactly
  once**, on a dedicated "save this now" screen
  (`templates/recovery_codes.html`) with a per-code Copy button. Only a
  salted hash of the code is ever stored (`authutil.HashPassword`, the
  same function used for the login password itself), so if the JSON data
  file ever leaks, the recovery codes inside it are no more exposed than
  the passwords are.
- `/forgot-password` asks for the username, that recovery code, and a new
  password (with confirmation, minimum 8 characters). On success, the
  password is updated **and** the recovery code is rotated — the old code
  stops working immediately, and a new one-time code is shown, so a
  single leaked/observed code can't be replayed later.
- The failure message is intentionally the same generic sentence whether
  the username doesn't exist or the code was wrong, so a failed attempt
  can't be used to enumerate valid usernames.
- If both people lose their code (e.g. neither saved it), the only
  recovery path in this MVP is direct access to `data/studylog.json` on
  the machine running the server — acceptable for a private, self-hosted,
  two-person tool, and called out explicitly on the login page's
  "forgot password" link rather than left as a dead end.

## 19. Data Persistence

See Decision D1 for *why* JSON-file storage was used instead of SQLite.
Mechanically: `store.Store` holds the entire dataset in memory as one Go
struct, guarded by a `sync.RWMutex`. Every mutating call (`CreateGoal`,
`UpdateSession`, ...) re-serializes the *whole* struct to JSON and writes
it via a temp-file-then-rename, so:

- A page reload never loses data (spec section 14) — it's read back from
  disk on every server start.
- A crash mid-write can't leave a half-written, corrupt file, because the
  rename is atomic on the same filesystem.

This does mean every write is O(total data size) rather than O(1) — for
two people logging study sessions for years, that's still a tiny file
(well under a megabyte for many years of daily logs), so this is a
non-issue at this app's actual scale, but would need to change before,
say, hundreds of users share one file.

## 20. Security Considerations

- **Scoping**: every store read/write for goals and sessions checks
  `resource.GroupID == currentUser.GroupID` before returning or mutating
  it, so one Study Group cannot see or edit another's data even though
  everything lives in one file (this only matters once StudyLog is ever
  run for more than one couple against the same data file/server — see
  Future Extensions for multi-group support).
- **Ownership**: `apiUpdateSession`/`apiDeleteSession` additionally check
  `session.UserID == currentUser.ID` — your partner can *see* your
  sessions (by design) but not edit or delete them.
- **XSS**: all user-authored text (goal titles/descriptions, session
  subjects/descriptions) is rendered through auto-escaping (`html/template`
  server-side, `textContent`/manual escaping client-side) — see section 17.
- **Cookies**: the session cookie is `HttpOnly` (not readable from JS)
  and `SameSite=Lax` (mitigates basic CSRF via cross-site navigation).
  There is no separate CSRF token on top of that; for a two-person
  private tool this is a deliberate simplicity/risk tradeoff, called out
  here rather than left unstated.
- **Password storage**: see section 18 for the explicit tradeoff made
  there.
- **Transport**: the app itself does not terminate TLS — it's meant to be
  run behind a reverse proxy (or just on `localhost`/a private network)
  for HTTPS in any real deployment.

## 21. Why Each Technology Was Chosen

- **Go, stdlib-only backend**: compiles to one binary, no `npm install`,
  no framework version treadmill, and every line of routing/auth/business
  logic is something a reader can open and read directly instead of
  trusting a framework's implicit behavior.
- **`html/template` server rendering**: for a two-person tool, most pages
  genuinely don't need to be a single-page app; server rendering means
  "view source" *is* mostly the real content, which is easier to debug.
- **Vanilla JS for the interactive parts**: only Grass and the two
  dialogs need `fetch()`-driven updates; that's a small enough surface
  area that React/Vue's benefits (component reuse, complex state) don't
  outweigh their cost (build step, bundle, dependency surface) here.
- **JSON-file store instead of SQLite**: see Decision D1.

## 22. Alternatives That Were Considered

- **SQLite via `mattn/go-sqlite3` (CGO) or `modernc.org/sqlite` (pure
  Go)** — the originally requested choice; not used in this build for
  environment reasons (Decision D1), but `store.Store`'s method set is
  intentionally shaped so a `database/sql`-backed reimplementation is a
  contained, single-package change.
- **Next.js/React frontend** — explicitly ruled out by the spec, and also
  genuinely unnecessary for this amount of UI.
- **A generic "metric" system on StudySession** (arbitrary
  `{unit: count}` pairs, so "20 problems" and "60 minutes" are both first
  class) — considered for sections 6/11, but deferred (Decision D2): it's
  the right eventual design, but building it now, before there's a second
  real unit to support, would be exactly the kind of speculative
  complexity the spec warns against.

## 23. How to Run Locally

Requirements: Go 1.22+ (for the stdlib `ServeMux` pattern routing used in
`internal/handlers/handlers.go`).

```bash
cd studylog
go run .
# -> StudyLog listening on :8080
```

Then open `http://localhost:8080` — the first visit redirects to
`/setup`, where you create the shared Group and the two accounts. After
that, `/login` is the entry point. Data is written to
`data/studylog.json` (path configurable via `-data`); delete that file to
reset the app to a fresh, empty state. The listen address is configurable
via `-addr` (default `:8080`).

## 24. How to Add a New Feature

Example: "let a Study Session record a count of problems solved, and let
a cumulative Goal's target be in `problems` instead of only minutes" (the
natural next step flagged in Decision D2):

1. **models.go** — add `ProblemsSolved int` to `StudySession`, and maybe
   a `TargetUnit`-driven branch in a new small `Metric(session) float64`
   helper.
2. **store.go** — no change needed; it serializes whatever's in the
   struct.
3. **grass.go** — `BuildCells` and `ComputeProgress` currently hardcode
   `sess.DurationMin`; change them to call the new `Metric()` helper so
   they work for either unit.
4. **handlers.go** — accept the new field in `sessionInput`, validate it.
5. **templates + JS** — add the input field to the "Add a session" form,
   and display the right unit label instead of always "min".

In general: **models** define the shape, **store** just persists
whatever shape exists, **grass** holds all the "what does this number
mean" logic, and **handlers** plus templates/JS are the thin plumbing on
top. Most features touch grass.go and handlers.go; store.go rarely needs
to change beyond adding a field to a struct.

## 25. Future Extensions

Explicitly out of scope for this MVP (per spec section 13), but the
architecture leaves room for:

- **Real SQL storage** (SQLite -> Postgres) once the environment allows
  pulling in a driver — swap `internal/store`'s internals only.
- **Multi-unit metrics** on StudySession (problems, pages, papers), per
  Decision D2 and section 24's worked example.
- **Weekly Grass** (currently only `TargetPeriod == "day"` goals appear
  in the Grass selector; weekly-target goals are stored but not yet
  visualized as a calendar heatmap).
- **Real password hashing** (bcrypt/argon2) if the app is ever deployed
  beyond a private two-person, trusted-network setting.
- **Multi-group support at the infrastructure level** (today, the data
  model already scopes everything by `GroupID`, so multiple couples
  *could* share one running server/data file safely — this just hasn't
  been tested at that scale, and `/setup` currently only bootstraps a
  single group per fresh data file).
- **Session editing UI** (`PUT /api/sessions/{id}` already exists
  server-side; the dashboard dialog currently only exposes "add" and
  "delete", not "edit", for the MVP).

---

## Known Issues Fixed After Launch

**"+ New goal" did nothing on a brand-new account (fixed).**
Go's `encoding/json` marshals a nil slice (`var out []T` that was never
appended to) as JSON `null`, not `[]`. `store.GoalsInGroup` (and the
other `*InGroup`/`ChildGoals` list methods) used exactly that pattern, so
`GET /api/goals` returned `null` for a brand-new group with zero goals —
i.e. precisely the moment a user first tries to create their *first*
goal. `goals.js` did `allGoals = await res.json()` and then immediately
called `.filter(...)` on it; calling an array method on `null` throws,
which silently aborted `openForCreate()` **before** `dialog.showModal()`
ever ran — so clicking "+ New goal" appeared to do nothing at all, with
no visible error.

Fix, in two layers:
1. **Root cause**: every store method that returns a list now
   initializes `out := []T{}` instead of `var out []T`, so an empty
   result always serializes as JSON `[]`. This is the correct general
   rule for this codebase — any new list-returning store method should
   follow the same pattern — and it's why `internal/handlers` never had
   to special-case "empty" responses.
2. **Defense in depth**: `loadGoals()` in both `static/js/app.js` and
   `static/js/goals.js` now does `allGoals = (await res.json()) || []`,
   so even an unexpected `null` from some future endpoint degrades to
   "no goals" instead of a thrown exception.

This was caught by writing a small Node/jsdom harness that loads the
real server-rendered `/goals` page, executes the real `goals.js`, and
simulates the click → fill form → save sequence exactly as a browser
would — reproducing the silent failure outside of manual browser testing.

## Decisions Log

This section records places where implementation revealed a genuine
ambiguity or constraint not fully resolved by the spec, per the
instruction to record such cases here rather than silently improvising.

**D1 — SQLite requested, JSON-file store used instead.**
The spec's first-choice stack is Go + SQLite. This build environment does
not have network access to the Go module proxy (or any path that resolves
Go vanity imports), so fetching either a CGO SQLite driver
(`mattn/go-sqlite3`) or a pure-Go one (`modernc.org/sqlite`) is not
reliably reproducible here. Rather than vendor a driver in a fragile,
undocumented way, StudyLog uses a small dependency-free JSON-file store
(`internal/store`) with an interface shaped like a minimal repository
layer, specifically so that swapping in `database/sql` plus a real
SQLite (and later Postgres) driver is a contained, single-package change
later — see sections 19 and 22. This also happens to align with the
spec's own "lightweight, easy to understand" priority: a developer can
read the entire persistence layer in one file with zero dependencies.

**D2 — targets/progress are minutes-only for now.**
The spec's own examples mix units ("60 min/day" vs "20 problems/day" vs
"5 papers/week"). Building a fully generic unit system before there's a
second real unit actually wired into logging would be exactly the kind
of speculative complexity the spec asks to avoid. `TargetUnit` is stored
and displayed as a label today; only minutes are functionally measured.
See section 5 and section 24 for the concrete extension path.

**D3 — Grass never aggregates a goal's descendants into one cell.**
Read literally, "select an abstract goal and see its Grass" could mean
either "let the user pick a measurable descendant instead" (what section
3 describes) or "sum all descendants' minutes into the abstract goal's
own cell" (which risks silently mixing incompatible measures, e.g.
minutes of Math + minutes of Research). The former was implemented,
matching the spec's explicit wording; the goal-progress bar (section 11),
by contrast, *does* aggregate descendants, because there the spec
explicitly asks for that, and progress is a single already-normalized
percentage rather than a mix of raw units.
