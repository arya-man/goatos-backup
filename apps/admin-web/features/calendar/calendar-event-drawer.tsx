import { randomUUID } from "node:crypto";
import Link from "next/link";
import { Bell, X } from "lucide-react";
import { Tag } from "@/components/ui-primitives";
import { dateTime, fmtDateTime } from "@/lib/format";
import { scopeHref, type Scope } from "@/lib/scope";
import { copy, optionalOption, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { sendNudgeAction, snoozeAction } from "./calendar-actions";
import {
  blockEntries,
  driveShedId,
  eventTypeMeta,
  hasWorkflowLink,
  ownerColor,
  ownerLabel,
  parseLinks,
  type CalendarDriveTarget,
  type CalendarEventDetail,
  type CalendarJSONBlock,
  type CalendarPresentation,
  type OwnerPresentationMap,
} from "./calendar-contract";
import { ProcurementPager } from "@/features/procurement/pager";

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

function contractStateLabel(pageContract: AdminUiPageContract, groupId: string, state: string | null | undefined, noneKey = "label.placeholder"): string {
  const key = state?.trim();
  if (!key || key === "none" || key === "not_scheduled") return copy(pageContract, noneKey);
  return optionalOption(pageContract, groupId, key)?.label ?? key.replace(/_/g, " ");
}

export function CalendarEventDrawer({
  detail,
  targets,
  targetsError,
  targetsNextHref,
  targetsPrevHref,
  targetsPage,
  targetsOnPage,
  closeHref,
  returnTo,
  scope,
  presentation,
  ownerMeta,
  pageContract,
}: {
  detail: CalendarEventDetail;
  targets: CalendarDriveTarget[] | null;
  targetsError: string | null;
  targetsNextHref: string | null;
  targetsPrevHref: string | null;
  targetsPage: number;
  targetsOnPage: number;
  closeHref: string;
  returnTo: string;
  scope: Scope;
  presentation: CalendarPresentation;
  ownerMeta: OwnerPresentationMap;
  pageContract: AdminUiPageContract;
}) {
  const event = detail.event;
  const typeMeta = eventTypeMeta(event.event_type, presentation);
  const TypeIcon = typeMeta.icon;
  const accent = ownerColor(event.owner_key, ownerMeta);
  const status = {
    label: optionLabel(pageContract, "calendar_status", event.status),
    tone: optionTone(pageContract, "calendar_status", event.status) as "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal",
  };
  const severity = {
    label: optionLabel(pageContract, "calendar_severity", event.severity),
    tone: optionTone(pageContract, "calendar_severity", event.severity) as "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal",
  };
  const channels = detail.notification_channels ?? [];
  const whenWindow = event.window_start && event.window_end ? `${fmtDateTime(event.window_start)} → ${fmtDateTime(event.window_end)}` : fmtDateTime(event.due_at);

  // Footer "Open drive" (shed execution) vs "Open workflow", from the event's link keys → app routes.
  const shedId = driveShedId(event);
  const driveHref = shedId ? scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(shedId)}`, scope) : undefined;
  const workflowHref = hasWorkflowLink(event) ? scopeHref(`/workflows/${encodeURIComponent(event.event_id)}`, scope, {}, { from: "calendar" }) : undefined;
  const primaryOpen = driveHref ? { href: driveHref, label: copy(pageContract, "action.open_drive") } : workflowHref ? { href: workflowHref, label: copy(pageContract, "action.open_workflow") } : null;
  const linkRow = parseLinks(event, pageContract);

  // Per-render idempotency keys: a double-submit of the SAME rendered form replays (no duplicate action).
  const nudgeKey = randomUUID();
  const snoozeKey = randomUUID();
  // snooze_until (default +24h) is computed in the snooze server action — Date.now() is an impure call and
  // is not allowed on the render path.

  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.event.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.event.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: `color-mix(in srgb, ${accent} 18%, var(--panel))`, color: accent }}>
            <TypeIcon className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.event.eyebrow")}</div>
            <h2>{event.title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.event.close_label")} scroll={false}>
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
            <MetaCell k={copy(pageContract, "label.when")} v={whenWindow} />
            <MetaCell k={copy(pageContract, "label.reminder")} v={contractStateLabel(pageContract, "calendar_reminder_state", event.reminder_state, "label.not_configured")} />
            <MetaCell k={copy(pageContract, "label.channel")} v={event.primary_notification_channel || <span className="muted">{copy(pageContract, "label.not_configured")}</span>} />
            <MetaCell k={copy(pageContract, "label.escalates")} v={contractStateLabel(pageContract, "calendar_escalation_state", event.escalation_state)} />
          </div>
          {channels.length > 1 ? (
            <div className="note" style={{ marginTop: 10 }}>
              {copy(pageContract, "label.channels")}: {channels.join(" · ")}
            </div>
          ) : null}

          {/* Scope card — from typed event fields (reliable). */}
          <div style={{ marginTop: 16 }}>
            <div className="b700" style={{ margin: "2px 0 8px" }}>
              {copy(pageContract, "label.scope")}
            </div>
            <div className="metagrid">
              <MetaCell k={copy(pageContract, "label.park_shed")} v={`${event.park_code ?? copy(pageContract, "label.placeholder")} · ${event.shed_name ?? copy(pageContract, "label.all_sheds")}`} />
              <MetaCell k={copy(pageContract, "label.cohort_target")} v={`${event.cohort_name ?? copy(pageContract, "label.placeholder")} · ${event.target_count}`} />
              <MetaCell k={copy(pageContract, "label.vaccine_dose")} v={`${event.vaccine_name ?? copy(pageContract, "label.placeholder")} · ${event.dose_code ?? copy(pageContract, "label.placeholder")}`} />
              <MetaCell k={copy(pageContract, "label.owner")} v={event.assignee_label ?? ownerLabel(event.owner_key, ownerMeta)} />
            </div>
          </div>

          {/* Detail blocks — rendered generically from the backend JSONBlocks (keys vary by event family). */}
          <BlockCard
            title={copy(pageContract, "label.source_backed_rule")}
            block={detail.source_and_rule}
            leading={<Tag tone={event.source_backed ? "ok" : "warn"}>{event.source_backed ? copy(pageContract, "label.source_backed") : copy(pageContract, "label.not_source_backed")}</Tag>}
          />
          <BlockCard title={copy(pageContract, "label.execution")} block={detail.execution} />
          {event.event_type === "vaccination_drive" ? (
            <div style={{ marginTop: 16 }}>
              <div className="b700" style={{ margin: "2px 0 8px" }}>
                {copy(pageContract, "calendar.drawer.eligible_goats")}
              </div>
              {targetsError ? (
                <div className="alert" style={{ marginBottom: 10 }}>
                  {targetsError}
                </div>
              ) : null}
              {targets && targets.length > 0 ? (
                <div className="bd" style={{ padding: 0, border: "1px solid var(--line2)", borderRadius: 10, overflow: "hidden" }}>
                  <table>
                    <thead>
                      <tr>
                        <th>{copy(pageContract, "label.rfid")}</th>
                        <th>{copy(pageContract, "label.stage")}</th>
                        <th>{copy(pageContract, "label.status")}</th>
                        <th>{copy(pageContract, "label.when")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {targets.map((row) => (
                        <tr key={row.obligation_id}>
                          <td>{row.rfid ?? copy(pageContract, "label.placeholder")}</td>
                          <td>{row.stage ?? copy(pageContract, "label.placeholder")}</td>
                          <td>
                            <Tag tone={optionTone(pageContract, "calendar_status", row.status) as "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal"}>
                              {optionLabel(pageContract, "calendar_status", row.status)}
                            </Tag>
                          </td>
                          <td className="muted small">{fmtDateTime(row.due_at)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  <ProcurementPager prevHref={targetsPrevHref} nextHref={targetsNextHref} page={targetsPage} count={targetsOnPage} noun="goat" />
                </div>
              ) : (
                <p className="muted small" style={{ margin: 0 }}>
                  {copy(pageContract, "calendar.drawer.eligible_goats_empty")}
                </p>
              )}
            </div>
          ) : null}
          <BlockCard title={copy(pageContract, "label.stock_readiness")} block={detail.stock} />
          <BlockCard title={copy(pageContract, "label.proof")} block={detail.proof} />
          <BlockCard title={copy(pageContract, "label.verification")} block={detail.verification} />

          {/* Recent activity — the event's audit/reminder/snooze/proof history timeline. */}
          {detail.recent_actions && detail.recent_actions.length ? (
            <div style={{ marginTop: 16 }}>
              <div className="b700" style={{ margin: "2px 0 8px" }}>
                {copy(pageContract, "label.recent_activity")}
              </div>
              <div className="feed">
                {detail.recent_actions.map((h) => (
                  <div key={h.history_id} className="fitem">
                    <div className="tx">
                      <b>{h.title}</b>
                      <div className="mt">
                        {[optionLabel(pageContract, "calendar_history_status", h.status), h.actor_label, h.channel].filter(Boolean).join(" · ")}
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
                {copy(pageContract, "label.linked")}
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
              {copy(pageContract, "action.send_nudge")}
            </button>
          </form>
          <form action={snoozeAction}>
            <input type="hidden" name="event_id" value={event.event_id} />
            <input type="hidden" name="idempotency_key" value={snoozeKey} />
            <input type="hidden" name="return_to" value={returnTo} />
            <button type="submit" className="btn">
              {copy(pageContract, "action.snooze")}
            </button>
          </form>
          {primaryOpen ? (
            <Link href={primaryOpen.href} className="btn">
              {primaryOpen.label}
            </Link>
          ) : null}
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.close")}
          </Link>
        </div>
      </aside>
    </>
  );
}
