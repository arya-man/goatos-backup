# PR #295 — USER-FACING COPY JUDGE

Worktree `/Users/raviteja/mesha/.worktrees/tasks-jira-ui`, branch `feat/tasks-jira-ui-20260918`,
HEAD **6f00fd82997c1ed4894df15c96f84945452d0993**, merge-base `36b98fd53058b14326ea8e134ea97fb27d8db20a`.
Judged **as on disk**, not as in the commit — two agents were writing while I read.

mtimes measured (IST, laptop clock):

| file | mtime |
|---|---|
| `apps/admin-web/features/notifications/notification-copy.ts` | 2026-09-18 03:38:03 |
| `backend/internal/adminui/app/service.go` | 2026-09-18 05:14:36 |
| `apps/admin-web/lib/push-copy.ts` | 2026-09-18 07:12:37 |
| `apps/admin-web/features/leadership-tasks/task-board-dnd.tsx` | 2026-09-18 08:09:34 |
| `apps/admin-web/features/leadership-tasks/task-presentation.ts` | 2026-09-18 08:10:44 |
| `apps/admin-web/features/notifications/mention-picker.tsx` | 2026-09-18 08:11:05 |
| `apps/admin-web/features/leadership-tasks/task-feedback-banner.tsx` | changed *during* this audit (see F1) |

**Mid-audit change, recorded:** `task-feedback-banner.tsx` and `task-presentation.ts` were both
rewritten while I was reading them. The banner now reads `statusNow`/`who` off the URL and fills
`{name}`/`{status}` templates — that is the right shape and I judged the new version. The
CEO-caught defect is **still live**, but it has moved one layer down (F1).

## Verification, with real output

| gate | result |
|---|---|
| `make notification-specificity-guard` | **PASS** (self-test PASS; 23 files in `notificationbridge`, no abstract copy) |
| `make ui-vaccine-labels-guard` | **PASS** (self-test PASS) |
| `make date-format-guard` | **PASS** (self-test PASS) — but see F2; the guard does not see this shape |
| `node apps/admin-web/scripts/check-ui-contract-literals.mjs` | **PASS** ("admin UI contract literal guard passed.") |
| pen-not-shed | there is **no `make` target**. Real targets: `go test ./internal/adminui/app -run 'TestBootstrapContractSaysPenNeverShed\|TestColumnLabelsSpeakPen'` → **both PASS**; `node --test apps/admin-web/features/counts/pen-vocabulary.test.mjs` → **1 pass, 0 fail** |
| `cd backend && go test ./internal/adminui/... -count=1` | **ok** ×3 packages, `domain` no test files |
| banned internal words in visible strings | **clean** — every diff hit is a test assertion message or a code identifier (`Idempotency-Key` header, `fixture-contract.ts` import, "the backend edit command" in an assert) |
| `#[0-9a-f]{3,6}` in visible copy | **clean** — zero hits across `apps/admin-web/features` + `backend/internal/adminui` |

**Verdict: the PR is largely clean on this axis.** The board, the detail panel, the toolbar, the
edit modal, the notification panel and the backend-composed push bodies all name their things.
Five findings below are genuine; the first two are the ones a CXO would be misled by.

---

# CONFIRMED

## F1 — The CEO's exact defect, now in the BACKEND contract, where it WINS
`backend/internal/adminui/app/service.go:2109` (`pageSpecificCopy("leadership-tasks")`)

```go
"feedback.version_conflict": "Someone changed this task while you were editing it. Reload the page and try again.",
```

This is the banned sentence verbatim: it names nobody and no status, and it tells the reader to
reload a page that `revalidatePath` has **already** reloaded. Worse, it is now actively harmful:
`task-feedback-banner.tsx` was just rewritten to try `feedback.version_conflict.named` →
`.status` → `feedback.version_conflict`, and `actions.ts:174-184` re-reads the task on the failure
path to put `task_now` (the current `status_chip`) and `task_who` on the redirect. **Neither
`.named` nor `.status` exists in the contract**, so every drag refusal falls through to this plain
key — and because `copy()` prefers the contract over the frontend fallback, the good fallback the
other agent just wrote (`"This task is already {status} — …"`) is **dead code on a served
backend**. The facts are fetched, put on the URL, and thrown away.

Same shape, same file, same map:
- `:2110` `"feedback.task_closed": "This task is already finished, so it can no longer be edited."` — says "edited" for a refusal that fires on a **drag**, and drops the status.
- `:2111` `"feedback.forbidden": "Only the person who raised this task can edit it."` — the system holds `raised_by_name`.
- `:2116` `"feedback.invalid_status_change": "That status change could not be applied."` — pure "gesture at it".
- There is **no `feedback.not_assignee` / `feedback.not_raiser` / `feedback.invalid_status_transition` key at all**, so the three commonest drag refusals have no contract sentence.

Belongs in the **backend contract** — `pageSpecificCopy("leadership-tasks")`. Proposed:

```diff
-			"feedback.version_conflict":        "Someone changed this task while you were editing it. Reload the page and try again.",
-			"feedback.task_closed":             "This task is already finished, so it can no longer be edited.",
-			"feedback.forbidden":               "Only the person who raised this task can edit it.",
-			"feedback.invalid_status_change":   "That status change could not be applied.",
+			// Three keys per refusal, most specific first: `.named` (name + status both
+			// resolved), `.status` (status only), then the plain key. The screen uses a
+			// template only when every placeholder it carries has a value, so an
+			// unresolvable name costs a clause and never renders "{name}" or an id.
+			"feedback.version_conflict.status": "This task is already {status} — it was moved while this board was open, and the board now shows that. Move it from what you see now.",
+			"feedback.version_conflict":        "This task was moved while this board was open, so your move was not applied. The board now shows the current version.",
+			"feedback.task_closed.status":      "This task is {status}, and a closed task cannot be moved.",
+			"feedback.task_closed":             "This task is closed, so its status cannot be changed.",
+			"feedback.not_assignee.named":      "Only {name}, the person this task is assigned to, can move it.",
+			"feedback.not_assignee":            "Only the person this task is assigned to can move it.",
+			"feedback.not_raiser.named":        "Only {name}, who raised this task, can cancel it.",
+			"feedback.not_raiser":              "Only the person who raised this task can cancel it.",
+			"feedback.invalid_status_transition.status": "This task is {status}, and that is not a move it can make from there.",
+			"feedback.invalid_status_transition":        "That is not a move this task can make from where it is now.",
+			"feedback.forbidden.named":         "Only {name}, who raised this task, can edit it.",
+			"feedback.forbidden":              "Only the person who raised this task can edit it.",
```

Note what still cannot be said and why: `actions.ts:165-170` records that neither the 409 envelope
nor `GET /app/leadership-tasks/{task_id}` reports **who** made the last status change, so
"Chandrakant already moved this to Doing" is not yet composable — the honest version names the
**new status**. The backend follow-up is `status_changed_by_name` on the task read; once it exists,
`feedback.version_conflict.named` becomes `"{name} already moved this to {status}. …"`. That is the
one change that closes the CEO's defect completely, and it is a backend change, not a copy change.

## F2 — All four @mention refusals reach the screen as one generic failure
`backend/internal/leadershiptasks/app/errors.go:65,67,72` and `app/service.go:343`

The backend sentences are decent-ish:

```go
BadRequest("too_many_mentions",   "Mention at most 20 people in one note.")
BadRequest("invalid_mention",     "One of the people you mentioned is not on this farm's team. Pick them from the list.")
Forbidden("mention_not_visible",  "That person cannot see this task, so they cannot be mentioned on it.")
BadRequest("mention_without_note","Write the note before naming someone in it.")
```

**None of them ever renders.** `actions.ts:234` stamps only `result.error.code` on the redirect;
`TaskFeedbackBanner` looks up `feedback.<code>`, and `pageSpecificCopy("leadership-tasks")` carries
no `feedback.mention_*` key and `REFUSAL_FALLBACKS` no mention entry — so all four collapse into
`copy(pageContract, "action.failed_message")`. A CXO who @-mentions the wrong person is told
"something went wrong" about a note they can see on screen.

Two of them also fail the lens on their own wording: `invalid_mention` says *"one of the people"*
and `mention_not_visible` says *"that person"* — the picker resolved these people by name, so the
name is in hand. Add the keys **and** make the backend errors carry the name.

Fix in the **backend contract**, keys `feedback.mention_not_visible` / `feedback.invalid_mention` /
`feedback.too_many_mentions` / `feedback.mention_without_note`:

```diff
+			"feedback.mention_not_visible.named": "{name} cannot see this task, so they cannot be named on it. Mention someone on this task's park scope, or share the task first.",
+			"feedback.mention_not_visible":       "One of the people you named cannot see this task, so they cannot be named on it.",
+			"feedback.invalid_mention.named":     "{name} is not on the leadership roster any more. Pick the person from the list that appears when you type @.",
+			"feedback.invalid_mention":           "Someone you named is not on the leadership roster. Pick the person from the list that appears when you type @.",
+			"feedback.too_many_mentions":         "A single update can name at most 20 people.",
+			"feedback.mention_without_note":      "Write the update before naming anyone in it.",
```

`{name}` needs the backend to put the offending display name on the error (the mention row already
resolves it under the task's row lock), then `actions.ts` to ride it on the redirect the way it
already does for `task_who`.

## F3 — The notification centre timestamp is not DD/MM/YYYY, and hand-rolls its own format
`apps/admin-web/features/notifications/notification-copy.ts:113-126`

```ts
return new Intl.DateTimeFormat(locale, {
  day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit",
  timeZone: "Asia/Kolkata",
}).format(value);
```

Renders `18 Sep, 14:32`. The 2026-09-10 maintainer lock says every visible timestamp is
`DD/MM/YYYY HH:MM` with slashes, in full, with **no compact variant** — the whole point of that
lock was that the console said `14-08-2026` while the phone said `14 Aug`, which is exactly what
this reintroduces one panel over. It also hand-rolls a date instead of calling
`apps/admin-web/lib/format.ts`. `make date-format-guard` passes because the guard's scan looks for
a JSX text node shipping a bare `*_date` field and for retired Kotlin/Go patterns, not for a fresh
`Intl.DateTimeFormat` inside a `.ts` helper — a stated blind spot, not a clean bill.

Frontend, and it stays frontend (the row's `created_at` is a wire instant; the helper is the right
place). Fix:

```diff
-export function formatNotificationTime(iso: string, locale?: string): string {
-  const value = new Date(iso);
-  if (Number.isNaN(value.getTime())) return "";
-  return new Intl.DateTimeFormat(locale, {
-    day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit",
-    timeZone: "Asia/Kolkata",
-  }).format(value);
-}
+import { fmtDateTime } from "@/lib/format";
+
+/** The row's timestamp, DD/MM/YYYY HH:MM on the farm clock — the one shape every screen uses. */
+export function formatNotificationTime(iso: string): string {
+  return fmtDateTime(iso);
+}
```

Drop the `locale` argument at the call site in `notification-panel.tsx` with it; a farm-readable
date is not a locale question, which is the other half of the same lock.

## F4 — The push control's two worst states render a raw engineering sentence, from a frontend literal
`apps/admin-web/lib/web-push-state.ts:273-274`, rendered at
`apps/admin-web/components/push-permission-prompt.tsx:206-211`

```ts
export const VAPID_KEY_UNUSABLE_MESSAGE =
  "This environment's browser-notification key is not a usable VAPID key, so notifications are off until it is corrected or removed.";
```

`unconfigured` and `unsupported` both render `{state.reason}` verbatim. "This environment's",
"VAPID key", "corrected or removed" is engineering talk on a CXO's top bar in every admin route —
the copy firewall bans exactly this, and `VAPID` is a protocol token, which the raw-token rule bans
on its own terms. The neighbouring `unsupported` reasons are milder but the same class:
`:65 "This browser does not support web push."` and `:71 "Notifications need a secure (https) connection."`

Compounding it: these are **frontend literals with no contract key at all**, unlike every other
string in this control, which goes through `pushCopy` → `chromeCopy()`. And `push.timed_out` /
`push.retry` exist in `lib/push-copy.ts` but were **not added to `chromeCopy()`** in
`service.go:403-421` — the diff adds eleven `push.*` keys and stops short of those two, so the two
newest states are frontend-owned forever.

Belongs in the **backend contract**, `chromeCopy()`:

```diff
		"push.blocked": "Notifications are blocked for this site. Turn them back on in your browser's site settings (the icon beside the address bar), then reload this page.",
+		// The retryable timeout: permission was granted and nothing is known to be wrong.
+		"push.timed_out": "That took too long and did not finish. Nothing is switched on yet — click again to retry.",
+		"push.retry":     "Try again",
+		// Nothing the reader can do; say that plainly and name who can.
+		"push.unconfigured": "Notifications are not switched on for this workspace yet. Ask your Goat OS administrator to turn them on.",
+		"push.unsupported":  "This browser cannot show notifications. Open the dashboard in Chrome on a laptop or Android phone to get them.",
+		"push.insecure":     "Notifications need a secure connection. Open the dashboard on its https address.",
```

and in the component, replace `{state.reason}` with
`copy(state.status === "unconfigured" ? "push.unconfigured" : "push.unsupported")`, keeping
`VAPID_KEY_UNUSABLE_MESSAGE` as a **log/diagnostic** string only — the shape check it documents is
worth keeping, the sentence is not for a reader.

## F5 — The board's column fallback can render a raw status token
`apps/admin-web/features/leadership-tasks/leadership-tasks-board.tsx:240-247`

```ts
return copy(pageContract, `board.column.${column}`, column);
```

The third argument is the column **key**, so an unlabelled column renders `in_progress` — a raw
wire token as presentation copy. The comment says only `cancelled` reaches this path and that is
true today, but the fallback is the branch that fires when the assumption stops holding, and it
fires with the worst possible value. `board.column.*` is also absent from
`pageSpecificCopy("leadership-tasks")` entirely, so `cancelled` is already living on the frontend
fallback.

Add the four keys to the **backend contract** and make the frontend fallback a sentence, never a key:

```diff
+			"board.column.todo":        "To do",
+			"board.column.in_progress": "Doing",
+			"board.column.done":        "Done",
+			"board.column.cancelled":   "Cancelled",
```
```diff
-  return copy(pageContract, `board.column.${column}`, column);
+  // Never the key: a status token is a wire value, not a column heading.
+  return copy(pageContract, `board.column.${column}`, "");
```
With an empty fallback the column renders unlabelled rather than leaking `in_progress`, and the
contract keys above mean it never actually does.

---

# SUSPECTED

## S1 — `+5` on the avatar group names nobody and is hidden from screen readers
`leadership-tasks-board.tsx:226-231` — `<span className="ltb-person ltb-person-rest" aria-hidden="true"><span className="lt-avx">+{hidden}</span></span>`

A count with no names and no `title`, and `aria-hidden` so it is not read at all. It is the
count-only shape in miniature. Marked SUSPECTED because it is an ornament beside a working
per-person filter rather than a refusal or a notification, so a CXO is not *misled*, only
under-served. A `title` naming the hidden people would close it:
`copy(pageContract, "board.people_rest", "{names} also have tasks here")` with the names joined
server-side. Contract key `board.people_rest`.

## S2 — `board.total_unavailable` explains the system, not the farm
`task-board-dnd.tsx:237-241` — fallback `"The whole-list total for this status is not published."`

"Published" is the backend's word for whether it emitted a count. A farm leader reads it as a
publishing action they might be able to take. Suggest `"The full count for this status is not
available right now — the number below is what is on this page."` under the existing key
`board.total_unavailable`, which is also absent from the contract and should be added to
`pageSpecificCopy("leadership-tasks")`.

## S3 — The live-region drag announcement is composed client-side and drops its preposition
`task-board-dnd.tsx:182-187` — `"Moving" + number + column.label` → "Moving TSK-102 Doing".

Three fragments concatenated in the client rather than one contract template. Reads as broken
English to a screen-reader user. A single key `board.drag_moving` taking placeholders —
`"Moving {task} to {status}"` — fixes both the grammar and the client-side composition. Contract
key `board.drag_moving` in `pageSpecificCopy("leadership-tasks")`.

---

# What I checked and found clean (not padding — these are the surfaces the brief named)

- **Backend-composed notification titles/bodies** (`notificationbridge/leadership_task_activity_notify_consumer.go:128-170`) are the best copy in the PR: `"Chandrakant named you on TSK-102"` / `"Task TSK-102, <title>. Chandrakant wrote on 18/09/2026: “…”. Open it to reply."` — real name, real task number, human title, farm date via `biztime.FarmDate` (verified: `farmDateOrToday` → `biztime.FarmDate`, `leadership_task_notify_consumer.go:310-315`). This is the shape F1 and F2 should copy.
- **Toolbar / filters / sort / edit modal** — `filter.assignee_pinned` ("This scope is already only your tasks."), `filter.range_note` ("Pick both ends of a date range — a half range is not applied."), `edit.deadline_hint` (names the farm clock and IST), `sort.*` ("Deadline soonest"). All backend-owned, all specific.
- **Empty states** — `empty.tasks_detail`, `empty.selected_detail`, `empty.activity`, `empty.attachments`, `board.column_empty`. Each says what would appear and why it is not there.
- **Notification panel / bell / mark-as-read** (`notification-copy.ts:18-38`) — `notifications.empty` names what lands there ("Task mentions and status changes addressed to you"); `notifications.unavailable` is honest about the account. Fine. (Its fallback-map-in-`lib/` shape and `notification-copy.ts` living in `features/` is a contract-ownership question for another judge, not a copy defect; the guard passes.)
- **Pen vs shed** — no positional violation anywhere in the diff; both enforcing tests pass. The `shed`s that appear are keys, columns and wire fields, correctly.
- **Dates elsewhere** — the deadline/raised-on renders go through `lib/format.ts`; only F3 hand-rolls.
- **No hex colours and no banned internal words in any visible string.**
