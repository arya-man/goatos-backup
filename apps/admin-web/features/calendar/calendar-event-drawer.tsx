import { randomUUID } from "node:crypto";
import Link from "next/link";
import { Bell, X } from "lucide-react";
import { Tag } from "@/components/ui-primitives";
import { dateTime, fmtDateTime } from "@/lib/format";
import { scopeHref, type Scope } from "@/lib/scope";
import { sendNudgeAction, snoozeAction } from "./calendar-actions";
import {
  blockEntries,
  driveShedId,
  eventTypeMeta,
  hasWorkflowLink,
  ownerColor,
  ownerLabel,
  parseLinks,
  severityMeta,
  stateLabel,
  statusMeta,
  type CalendarEventDetail,
  type CalendarJSONBlock,
  type CalendarPresentation,
  type OwnerPresentationMap,
} from "./calendar-contract";

function MetaCell({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div>
      <div className="k">{k}</div>
      <div className="v">{v}</div>
    </div>
  );
}

// Render one detail JSONBlock as a metagrid card; hidden when the block has no scalar entries.
function BlockCard({ title, block, leading }: { title: string; block: CalendarJSONBlock | undefined | null; leading?: React.ReactNode }) {
  const entries = blockEntries(block);
  if (entries.length === 0 && !leading) return null;
  return (
    <div style={{ marginTop: 16 }}>
      <div className="b700" style={{ margin: "2px 0 8px" }}>
        {title}
      </div>
      {leading ? <div style={{ marginBottom: entries.length ? 8 : 0 }}>{leading}</div> : null}
      {entries.length ? (
        <div className="metagrid">
          {entries.map((e) => (
            <MetaCell key={e.label} k={e.label} v={e.value} />
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function CalendarEventDrawer({
  detail,
  closeHref,
  returnTo,
  scope,
  presentation,
  ownerMeta,
}: {
  detail: CalendarEventDetail;
  closeHref: string;
  returnTo: string;
  scope: Scope;
  presentation: CalendarPresentation;
  ownerMeta: OwnerPresentationMap;
}) {
  const event = detail.event;
  const typeMeta = eventTypeMeta(event.event_type, presentation);
  const TypeIcon = typeMeta.icon;
  const accent = ownerColor(event.owner_key, ownerMeta);
  const status = statusMeta(event.status);
  const severity = severityMeta(event.severity);
  const channels = detail.notification_channels ?? [];
  const whenWindow = event.window_start && event.window_end ? `${fmtDateTime(event.window_start)} → ${fmtDateTime(event.window_end)}` : fmtDateTime(event.due_at);

  // Footer "Open drive" (shed execution) vs "Open workflow", from the event's link keys → app routes.
  const shedId = driveShedId(event);
  const driveHref = shedId ? scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(shedId)}`, scope) : undefined;
  const workflowHref = hasWorkflowLink(event) ? scopeHref(`/workflows/${encodeURIComponent(event.event_id)}`, scope, {}, { from: "calendar" }) : undefined;
  const primaryOpen = driveHref ? { href: driveHref, label: "Open drive" } : workflowHref ? { href: workflowHref, label: "Open workflow" } : null;
  const linkRow = parseLinks(event);

  // Per-render idempotency keys: a double-submit of the SAME rendered form replays (no duplicate action).
  const nudgeKey = randomUUID();
  const snoozeKey = randomUUID();
  // snooze_until (default +24h) is computed in the snooze server action — Date.now() is an impure call and
  // is not allowed on the render path.

  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close Calendar event drawer" scroll={false} />
      <aside className="drawer on" aria-label="Calendar event">
        <div className="dh">
          <span className="fic" style={{ background: `color-mix(in srgb, ${accent} 18%, var(--panel))`, color: accent }}>
            <TypeIcon className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">CALENDAR EVENT</div>
            <h2>{event.title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close Calendar event drawer" scroll={false}>
            <X className="ic" />
          </Link>
        </div>

        <div className="dc">
          <div className="chipset" style={{ marginBottom: 14 }}>
            <Tag tone="mut">{typeMeta.label}</Tag>
            <Tag tone={status.tone}>{status.label}</Tag>
            <Tag tone={severity.tone}>{severity.label}</Tag>
            <Tag tone="info">{ownerLabel(event.owner_key, ownerMeta)}</Tag>
          </div>

          {/* Mock metagrid: When / Reminder / Channel / Escalates. Channel from the API summary field. */}
          <div className="metagrid">
            <MetaCell k="When" v={whenWindow} />
            <MetaCell k="Reminder" v={stateLabel(event.reminder_state)} />
            <MetaCell k="Channel" v={event.primary_notification_channel || <span className="muted">not configured</span>} />
            <MetaCell k="Escalates" v={stateLabel(event.escalation_state, "—")} />
          </div>
          {channels.length > 1 ? (
            <div className="note" style={{ marginTop: 10 }}>
              Channels: {channels.join(" · ")}
            </div>
          ) : null}

          {/* Scope card — from typed event fields (reliable). */}
          <div style={{ marginTop: 16 }}>
            <div className="b700" style={{ margin: "2px 0 8px" }}>
              Scope
            </div>
            <div className="metagrid">
              <MetaCell k="Park · Shed" v={`${event.park_code ?? "—"} · ${event.shed_name ?? "all sheds"}`} />
              <MetaCell k="Cohort · Target" v={`${event.cohort_name ?? "—"} · ${event.target_count}`} />
              <MetaCell k="Vaccine · Dose" v={`${event.vaccine_name ?? "—"} · ${event.dose_code ?? "—"}`} />
              <MetaCell k="Owner" v={event.assignee_label ?? ownerLabel(event.owner_key, ownerMeta)} />
            </div>
          </div>

          {/* Detail blocks — rendered generically from the backend JSONBlocks (keys vary by event family). */}
          <BlockCard
            title="Source-backed rule"
            block={detail.source_and_rule}
            leading={<Tag tone={event.source_backed ? "ok" : "warn"}>{event.source_backed ? "source-backed" : "not source-backed"}</Tag>}
          />
          <BlockCard title="Execution" block={detail.execution} />
          <BlockCard title="Stock readiness" block={detail.stock} />
          <BlockCard title="Proof" block={detail.proof} />
          <BlockCard title="Verification" block={detail.verification} />

          {/* Recent activity — the event's audit/reminder/snooze/proof history timeline. */}
          {detail.recent_actions && detail.recent_actions.length ? (
            <div style={{ marginTop: 16 }}>
              <div className="b700" style={{ margin: "2px 0 8px" }}>
                Recent activity
              </div>
              <div className="feed">
                {detail.recent_actions.map((h) => (
                  <div key={h.history_id} className="fitem">
                    <div className="tx">
                      <b>{h.title}</b>
                      <div className="mt">
                        {[stateLabel(h.status), h.actor_label, h.channel].filter(Boolean).join(" · ")}
                      </div>
                    </div>
                    <span className="tm">{dateTime(h.occurred_at)}</span>
                  </div>
                ))}
              </div>
            </div>
          ) : null}

          {/* Linked surfaces — key→app-route, scope preserved; only confidently-mapped keys render. */}
          {linkRow.length ? (
            <div style={{ marginTop: 16 }}>
              <div className="b700" style={{ margin: "2px 0 8px" }}>
                Linked
              </div>
              <div style={{ display: "flex", gap: 7, flexWrap: "wrap" }}>
                {linkRow.map((l) => (
                  <Link key={l.key} href={scopeHref(l.appPath, scope)} className={`tag t-${l.tone}`}>
                    {l.label}
                  </Link>
                ))}
              </div>
            </div>
          ) : null}
        </div>

        {/* Footer — Send nudge / Snooze are real idempotent backend actions; Open drive/workflow deep-links. */}
        <div className="df">
          <form action={sendNudgeAction} style={{ flex: 1, display: "flex" }}>
            <input type="hidden" name="event_id" value={event.event_id} />
            <input type="hidden" name="idempotency_key" value={nudgeKey} />
            <input type="hidden" name="return_to" value={returnTo} />
            <button type="submit" className="btn p" style={{ flex: 1 }}>
              <Bell className="ic" aria-hidden="true" />
              Send nudge
            </button>
          </form>
          <form action={snoozeAction}>
            <input type="hidden" name="event_id" value={event.event_id} />
            <input type="hidden" name="idempotency_key" value={snoozeKey} />
            <input type="hidden" name="return_to" value={returnTo} />
            <button type="submit" className="btn">
              Snooze
            </button>
          </form>
          {primaryOpen ? (
            <Link href={primaryOpen.href} className="btn">
              {primaryOpen.label}
            </Link>
          ) : null}
          <Link href={closeHref} replace className="btn" scroll={false}>
            Close
          </Link>
        </div>
      </aside>
    </>
  );
}
