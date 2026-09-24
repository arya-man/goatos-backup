# Operations logic cards (DLQ Center, CEO AI trace)

Index: O1 DLQ KPI tiles | O2 DLQ list + filters | O3 DLQ drawer, replay / discard | O4 CEO AI admin trace viewer (Go-only)

Verified read-only on goatos-stg, 25/09/2026.

## O1 DLQ KPI tiles (`/operations/dlq`)
- Screen: `app/(admin)/operations/dlq/page.tsx`, UI `features/operations-dlq/index.tsx:89-92` (Dead letter, Failed, Discarded, Rows in view).
- Endpoint: `GET /operations/dlq?status&event_type&topic&limit=100` (`backend/internal/outbox/adapters/http/handler.go:55,88`); SQL `outbox/adapters/postgres/repository.go:360-400` over `outbox_messages`.
- Formula: each tile = rows of that status **in the fetched list**. The list fetches ONE status (tab, default `dead_letter`), max 100, so only the active tab's tile is non-zero. Rows in view = after client search.
- Status meaning (`outbox/app/service.go:170-220`): `failed` = permanent (invalid_event_envelope or publish_permanent_failure); `dead_letter` = retries exhausted (max 5 attempts); `discarded` = operator discarded.
- SQL (verified: dead_letter 0, failed 1200 (all `invalid_event_envelope`, attempt 1, created 25/07-19/09), discarded 0; pending 0; published 361,851):
```sql
SELECT status, count(*) FROM outbox_messages GROUP BY 1;
```
- Questions: "Are any events stuck?" / "Koi event atka hua hai kya?"

## O2 DLQ list + filters
- Columns (`index.tsx:136-160`, labels from page contract `dlq-events`): event type + event id, topic + status tag, attempts, replays, last error, updated (`index.tsx:178-203`).
- Filters -> SQL: status tab -> `status = $2`; event_type -> exact `event_type`; topic -> exact `topic`; order `updated_at DESC, outbox_id DESC`, limit 100. Search `q` is client-side over the 100 rows (`index.tsx:236`).
- SQL (verified failed by event: feed.wastage.completed 798, goat.obligations_canceled 197, weighing.observation_accepted 173, calendar.reminder.cadence.queued 13, goat.identity.changed 11):
```sql
SELECT event_type, topic, count(*) FROM outbox_messages WHERE status='failed' GROUP BY 1,2 ORDER BY 3 DESC;
```
- Trap: "failed" events were never delivered to consumers; downstream read models may miss them (e.g. 798 wastage events).
- Questions: "Which events failed to publish?" / "Kaunse events fail hue?"

## O3 DLQ drawer, replay / discard
- Drawer `features/operations-dlq/dlq-local-drawer.tsx:105-150`: status, event_id, aggregate, idempotency_key, trace, created/updated, attempts, replays, last_error, headers, payload.
- Actions: `POST /operations/dlq/replay|discard` (`handler.go:56-57`) with a required reason. Replay: status -> pending, `replay_count+1` for rows in (dead_letter, failed) with `replay_count` under the replay cap (`repository.go:479-505`); discard -> discarded (`:510-535`); each writes `outbox_dlq_actions` (`:570`).
- SQL (verified: 0 actions ever; sum replay_count on failed = 0): `SELECT action, count(*) FROM outbox_dlq_actions GROUP BY 1;`
- Questions: "Has anyone replayed failed events?" / "Failed events ko kisi ne replay kiya?"

## O4 CEO AI admin trace viewer (`/ceo-ai-admin`) — Go-only
- Screen: `app/(admin)/ceo-ai-admin/page.tsx`, `features/ceo-ai-admin/trace-viewer.tsx:160-225` (Status, Route tier, Tool, Latency, Rows, Actor role, Model, Prompt, Review verdict, per-step sub-question/tool/duration/rows/verdict).
- Endpoint: `GET /ceo-ai/admin/trace/{request_id}` (`backend/internal/ceoai/adapters/http/admin_trace.go:45`, route `routes.go:20`); superadmin only, 403 otherwise.
- Code: `ceoai/adapters/observability/store.go:113` latest row of `ceo_ai_assistant_audit` for (tenant, request_id); `step_trace` JSON -> steps; sanitized before return.
- Go-only: the read-only DB role gets `permission denied` on `ceo_ai_assistant_audit`; no business figures, one request at a time.
- Questions: "Why did the assistant answer this way?" / "Is sawaal ka jawab kaise nikla?" (admin only)
