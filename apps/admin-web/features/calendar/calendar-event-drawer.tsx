"use client";

import Link from "@/components/no-prefetch-link";
import {
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  currentHistoryEntryIsLocalOverlay,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { ArrowLeft, ArrowRight, Bell, X } from "lucide-react";
import { Tag } from "@/components/ui-primitives";
import { dateTime, fmtDateTime } from "@/lib/format";
import { scopeHref, type Scope } from "@/lib/scope";
import {
  copy,
  optionalCopy,
  optionLabel,
  optionTone,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { sendNudgeAction, snoozeAction } from "./calendar-actions";
import { operationalLocationLabel } from "@/lib/operational-location";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import {
  blockEntries,
  contractStateLabel,
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

export type CalendarDrawerLoadResult = {
  eventId: string;
  detail: CalendarEventDetail | null;
  detailError: string | null;
  targets: CalendarDriveTarget[] | null;
  targetsError: string | null;
  targetsNextCursor: string | null;
};

type CalendarDrawerLoader = {
  (eventId: string, includeTargets: boolean, targetsCursor?: string): Promise<CalendarDrawerLoadResult>;
};

function MetaCell({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div>
      <div className="k">{k}</div>
      <div className="v">{v}</div>
    </div>
  );
}

// Render one detail JSONBlock as a metagrid card; hidden when the block has no scalar entries.
function BlockCard({
  title,
  block,
  leading,
}: {
  title: string;
  block: CalendarJSONBlock | undefined | null;
  leading?: React.ReactNode;
}) {
  const entries = blockEntries(block);
  if (entries.length === 0 && !leading) return null;
  return (
    <div style={{ marginTop: 16 }}>
      <div className="b700" style={{ margin: "2px 0 8px" }}>
        {title}
      </div>
      {leading ? (
        <div style={{ marginBottom: entries.length ? 8 : 0 }}>{leading}</div>
      ) : null}
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

function allTargetsHaveStatus(
  targets: CalendarDriveTarget[] | null,
  status: string,
): boolean {
  return Boolean(
    targets?.length && targets.every((target) => target.status === status),
  );
}

function hasHeldTargets(targets: CalendarDriveTarget[] | null): boolean {
  return Boolean(
    targets?.some(
      (target) => target.status === "deferred" || target.status === "blocked",
    ),
  );
}

function driveTargetHeadingKey(
  event: CalendarEventDetail["event"],
  targets: CalendarDriveTarget[] | null,
): string {
  if (event.status === "deferred" || allTargetsHaveStatus(targets, "deferred"))
    return "calendar.drawer.deferred_animals";
  if (event.status === "blocked" || allTargetsHaveStatus(targets, "blocked"))
    return "calendar.drawer.blocked_animals";
  if (hasHeldTargets(targets)) return "calendar.drawer.linked_animals";
  return "calendar.drawer.eligible_animals";
}

function driveTargetHeading(
  pageContract: AdminUiPageContract,
  event: CalendarEventDetail["event"],
  targets: CalendarDriveTarget[] | null,
): string {
  return (
    optionalCopy(pageContract, driveTargetHeadingKey(event, targets)) ??
    copy(pageContract, "calendar.drawer.eligible_animals")
  );
}

function driveTargetReason(target: CalendarDriveTarget): string {
  if (target.status === "deferred" && target.defer_reason) return target.defer_reason;
  if (target.exit_reason) return target.exit_reason;
  return "";
}

export function CalendarEventDrawer({
  initialData,
  initialSelectedEventId,
  loadDrawer,
  includeTargets,
  closeHref,
  returnTo,
  scope,
  presentation,
  ownerMeta,
  pageContract,
}: {
  initialData: CalendarDrawerLoadResult | null;
  initialSelectedEventId?: string;
  loadDrawer: CalendarDrawerLoader;
  includeTargets: boolean;
  closeHref: string;
  returnTo: string;
  scope: Scope;
  presentation: CalendarPresentation;
  ownerMeta: OwnerPresentationMap;
  pageContract: AdminUiPageContract;
}) {
  const [displayedData, setDisplayedData] = useState<CalendarDrawerLoadResult | null>(initialData);
  const [drawerOpen, setDrawerOpen] = useState(Boolean(initialData));
  const [targetsPage, setTargetsPage] = useState(1);
  const [targetsCursor, setTargetsCursor] = useState<string | undefined>(undefined);
  const [targetsCursorStack, setTargetsCursorStack] = useState<Array<string | undefined>>([]);
  const [targetsLoading, setTargetsLoading] = useState(false);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const selectedIdRef = useRef(initialSelectedEventId ?? initialData?.eventId);
  const requestSequenceRef = useRef(0);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const showDrawer = useCallback((result: CalendarDrawerLoadResult) => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    setDisplayedData(result);
    openFrameRef.current = window.requestAnimationFrame(() => {
      setDrawerOpen(true);
      openFrameRef.current = null;
    });
  }, []);

  const hideDrawer = useCallback(() => {
    requestSequenceRef.current += 1;
    selectedIdRef.current = undefined;
    setTargetsLoading(false);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    setDrawerOpen(false);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedData(null);
      closeTimerRef.current = null;
    }, 280);
  }, []);

  useEffect(() => () => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
  }, []);

  useEffect(() => {
    async function syncSelectionFromUrl(): Promise<void> {
      const url = new URL(window.location.href);
      const hash = new URLSearchParams(url.hash.replace(/^#/, ""));
      const eventId = hash.get("calendar_event") ?? url.searchParams.get("event") ?? undefined;
      if (!eventId) {
        hideDrawer();
        return;
      }
      if (selectedIdRef.current === eventId) return;
      previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      selectedIdRef.current = eventId;
      setTargetsPage(1);
      setTargetsCursor(undefined);
      setTargetsCursorStack([]);
      setTargetsLoading(false);
      const sequence = ++requestSequenceRef.current;
      const result = await loadDrawer(eventId, includeTargets);
      if (requestSequenceRef.current !== sequence || selectedIdRef.current !== eventId) return;
      showDrawer(result);
    }
    function sync(): void {
      void syncSelectionFromUrl();
    }
    window.addEventListener("popstate", sync);
    window.addEventListener("hashchange", sync);
    window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, sync);
    const initialFrame = window.requestAnimationFrame(sync);
    return () => {
      window.cancelAnimationFrame(initialFrame);
      window.removeEventListener("popstate", sync);
      window.removeEventListener("hashchange", sync);
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, sync);
    };
  }, [displayedData, hideDrawer, includeTargets, loadDrawer, showDrawer]);

  useEffect(() => {
    if (drawerOpen) {
      const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
      return () => window.cancelAnimationFrame(frame);
    }
    previousFocusRef.current?.focus();
  }, [drawerOpen]);

  const closeDrawer = useCallback(() => {
    hideDrawer();
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref, hideDrawer]);

  useEffect(() => {
    if (!drawerOpen) return undefined;
    function onKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeDrawer();
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [closeDrawer, drawerOpen]);

  const loadTargetsPage = useCallback(async (cursor: string | undefined, page: number, stack: Array<string | undefined>) => {
    const eventId = selectedIdRef.current;
    if (!eventId) return;
    const sequence = ++requestSequenceRef.current;
    setTargetsLoading(true);
    const result = await loadDrawer(eventId, includeTargets, cursor);
    if (requestSequenceRef.current !== sequence || selectedIdRef.current !== eventId) return;
    setDisplayedData(result);
    setTargetsCursor(cursor);
    setTargetsPage(page);
    setTargetsCursorStack(stack);
    setTargetsLoading(false);
  }, [includeTargets, loadDrawer]);

  function nextTargetsPage(): void {
    if (!displayedData?.targetsNextCursor || targetsLoading) return;
    void loadTargetsPage(
      displayedData.targetsNextCursor,
      targetsPage + 1,
      [...targetsCursorStack, targetsCursor],
    );
  }

  function previousTargetsPage(): void {
    if (targetsPage <= 1 || targetsLoading) return;
    const stack = [...targetsCursorStack];
    const cursor = stack.pop();
    void loadTargetsPage(cursor, targetsPage - 1, stack);
  }

  if (!displayedData) return null;

  return (
    <CalendarEventDrawerPanel
      key={displayedData.eventId}
      data={displayedData}
      open={drawerOpen}
      closeDrawer={closeDrawer}
      closeButtonRef={closeButtonRef}
      targetsPage={targetsPage}
      targetsLoading={targetsLoading}
      onPreviousTargets={previousTargetsPage}
      onNextTargets={nextTargetsPage}
      returnTo={returnTo}
      scope={scope}
      presentation={presentation}
      ownerMeta={ownerMeta}
      pageContract={pageContract}
    />
  );
}

function CalendarEventDrawerPanel({
  data,
  open,
  closeDrawer,
  closeButtonRef,
  targetsPage,
  targetsLoading,
  onPreviousTargets,
  onNextTargets,
  returnTo,
  scope,
  presentation,
  ownerMeta,
  pageContract,
}: {
  data: CalendarDrawerLoadResult;
  open: boolean;
  closeDrawer: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
  targetsPage: number;
  targetsLoading: boolean;
  onPreviousTargets: () => void;
  onNextTargets: () => void;
  returnTo: string;
  scope: Scope;
  presentation: CalendarPresentation;
  ownerMeta: OwnerPresentationMap;
  pageContract: AdminUiPageContract;
}) {
  const { detail, targets, targetsError } = data;
  const idSeed = useId();

  if (!detail) {
    return (
      <>
        <button type="button" className="veil" hidden={!open} aria-label={copy(pageContract, "drawer.event.close_label")} onClick={closeDrawer} />
        <aside className={`drawer${open ? " on" : ""}`} role="dialog" aria-hidden={!open} inert={!open} aria-label={copy(pageContract, "drawer.event.aria")}>
          <div className="dh">
            <div>
              <div className="mt">{copy(pageContract, "drawer.event.eyebrow")}</div>
              <h2>{copy(pageContract, "drawer.event.aria")}</h2>
            </div>
            <span className="sp" style={{ flex: 1 }} />
            <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.event.close_label")} onClick={closeDrawer}><X className="ic" /></button>
          </div>
          <div className="dc"><div className="alert">{data.detailError}</div></div>
        </aside>
      </>
    );
  }

  const event = detail.event;
  const typeMeta = eventTypeMeta(event.event_type, presentation);
  const TypeIcon = typeMeta.icon;
  const accent = ownerColor(event.owner_key, ownerMeta);
  const status = {
    label: optionLabel(pageContract, "calendar_status", event.status),
    tone: optionTone(pageContract, "calendar_status", event.status) as
      "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal",
  };
  const severity = {
    label: optionLabel(pageContract, "calendar_severity", event.severity),
    tone: optionTone(pageContract, "calendar_severity", event.severity) as
      "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal",
  };
  const channels = detail.notification_channels ?? [];
  const whenWindow =
    event.window_start && event.window_end
      ? `${fmtDateTime(event.window_start)} → ${fmtDateTime(event.window_end)}`
      : fmtDateTime(event.due_at);

  // Footer "Open drive" (shed execution) vs "Open workflow", from the event's link keys → app routes.
  const shedId = driveShedId(event);
  const driveHref = shedId
    ? scopeHref(
        `/vaccination/execution/sheds/${encodeURIComponent(shedId)}`,
        scope,
      )
    : undefined;
  const workflowHref = hasWorkflowLink(event)
    ? scopeHref(
        `/workflows/${encodeURIComponent(event.event_id)}`,
        scope,
        {},
        { from: "calendar" },
      )
    : undefined;
  const primaryOpen = driveHref
    ? { href: driveHref, label: copy(pageContract, "action.open_drive") }
    : workflowHref
      ? {
          href: workflowHref,
          label: copy(pageContract, "action.open_workflow"),
        }
      : null;
  const linkRow = parseLinks(event, pageContract);

  // Stable for this mounted event drawer, so an accidental double-submit replays.
  const nudgeKey = `calendar-nudge-${event.event_id}-${idSeed}`;
  const snoozeKey = `calendar-snooze-${event.event_id}-${idSeed}`;
  const isClosedHistory = event.status === "completed" || event.status === "canceled";
  const isCatchupSummary = event.event_id.startsWith("catchup:");
  // snooze_until (default +24h) is computed in the snooze server action — Date.now() is an impure call and
  // is not allowed on the render path.

  return (
    <>
      <button
        type="button"
        className="veil"
        hidden={!open}
        aria-label={copy(pageContract, "drawer.event.close_label")}
        onClick={closeDrawer}
      />
      <aside
        className={`drawer${open ? " on" : ""}`}
        role="dialog"
        aria-label={copy(pageContract, "drawer.event.aria")}
        aria-hidden={!open}
        inert={!open}
      >
        <div className="dh">
          <span
            className="fic"
            style={{
              background: `color-mix(in srgb, ${accent} 18%, var(--panel))`,
              color: accent,
            }}
          >
            <TypeIcon className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">
              {copy(pageContract, "drawer.event.eyebrow")}
            </div>
            <h2>{event.title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button
            ref={closeButtonRef}
            type="button"
            className="iconbtn"
            aria-label={copy(pageContract, "drawer.event.close_label")}
            onClick={closeDrawer}
          >
            <X className="ic" />
          </button>
        </div>

        <div className="dc">
          <div className="chipset" style={{ marginBottom: 14 }}>
            <Tag tone="mut">{typeMeta.label}</Tag>
            <Tag tone={status.tone}>{status.label}</Tag>
            <Tag tone={severity.tone}>{severity.label}</Tag>
            <Tag tone="info">{ownerLabel(event.owner_key, ownerMeta)}</Tag>
            {event.all_day ? <Tag tone="info">{copy(pageContract, "calendar.drive.all_day")}</Tag> : null}
          </div>

          {event.aggregated ? (
            <div style={{ marginBottom: 16 }}>
              <div className="metagrid">
                <MetaCell k={copy(pageContract, "calendar.drive.sheds")} v={event.shed_count} />
                <MetaCell k={copy(pageContract, "calendar.drive.vaccines")} v={event.vaccine_count} />
                <MetaCell k={copy(pageContract, "calendar.drive.doses")} v={event.target_count} />
                <MetaCell k={copy(pageContract, "calendar.drive.packets")} v={event.drive_count} />
              </div>
              {event.vaccine_labels.length ? (
                <div style={{ marginTop: 12 }}>
                  <div className="b700" style={{ marginBottom: 8 }}>{copy(pageContract, "calendar.drive.vaccine_mix")}</div>
                  <div className="chipset">
                    {event.vaccine_labels.map((label) => <Tag key={label} tone="info">{label}</Tag>)}
                  </div>
                </div>
              ) : null}
              {event.shed_labels.length ? (
                <div style={{ marginTop: 12 }}>
                  <div className="b700" style={{ marginBottom: 8 }}>{copy(pageContract, "calendar.drive.shed_coverage")}</div>
                  <div className="chipset">
                    {event.shed_labels.map((label) => <Tag key={label} tone="mut">{label}</Tag>)}
                  </div>
                </div>
              ) : null}
            </div>
          ) : null}

          {/* Mock metagrid: When / Reminder / Channel / Escalates. Channel from the API summary field. */}
          <div className="metagrid">
            <MetaCell k={copy(pageContract, "label.when")} v={whenWindow} />
            <MetaCell
              k={copy(pageContract, "label.reminder")}
              v={contractStateLabel(
                pageContract,
                "calendar_reminder_state",
                event.reminder_state,
                "label.not_configured",
              )}
            />
            <MetaCell
              k={copy(pageContract, "label.channel")}
              v={
                event.primary_notification_channel || (
                  <span className="muted">
                    {copy(pageContract, "label.not_configured")}
                  </span>
                )
              }
            />
            <MetaCell
              k={copy(pageContract, "label.escalates")}
              v={contractStateLabel(
                pageContract,
                "calendar_escalation_state",
                event.escalation_state,
              )}
            />
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
              <MetaCell
                k={copy(pageContract, "label.park_shed")}
                v={`${event.park_code ?? copy(pageContract, "label.placeholder")} · ${
                  event.shed_name
                    ? event.operational_location_display ||
                      operationalLocationLabel({
                        shedName: event.shed_name,
                        partitionLabel: event.partition_label,
                        sourceShedName: event.source_shed_name,
                      })
                    : copy(pageContract, "label.all_sheds")
                }`}
              />
              <MetaCell
                k={copy(pageContract, "label.cohort_target")}
                v={`${event.cohort_name ?? copy(pageContract, "label.placeholder")} · ${event.target_count}`}
              />
              <MetaCell
                k={copy(pageContract, "label.vaccine_dose")}
                v={`${event.vaccine_name ?? copy(pageContract, "label.placeholder")} · ${event.dose_code ?? copy(pageContract, "label.placeholder")}`}
              />
              <MetaCell
                k={copy(pageContract, "label.owner")}
                v={
                  event.assignee_label ?? ownerLabel(event.owner_key, ownerMeta)
                }
              />
            </div>
          </div>

          {/* Detail blocks — rendered generically from the backend JSONBlocks (keys vary by event family). */}
          <BlockCard
            title={copy(pageContract, "label.source_backed_rule")}
            block={detail.source_and_rule}
            leading={
              <Tag tone={event.source_backed ? "ok" : "warn"}>
                {event.source_backed
                  ? copy(pageContract, "label.source_backed")
                  : copy(pageContract, "label.not_source_backed")}
              </Tag>
            }
          />
          <BlockCard
            title={copy(pageContract, "label.execution")}
            block={detail.execution}
          />
          {event.event_type === "vaccination_drive" ? (
            <div style={{ marginTop: 16 }}>
              <div className="b700" style={{ margin: "2px 0 8px" }}>
                {driveTargetHeading(pageContract, event, targets)}
              </div>
              {targetsError ? (
                <div className="alert" style={{ marginBottom: 10 }}>
                  {targetsError}
                </div>
              ) : null}
              {targets && targets.length > 0 ? (
                <div
                  className="bd"
                  style={{
                    padding: 0,
                    border: "1px solid var(--line2)",
                    borderRadius: 10,
                    overflowX: "auto",
                    overflowY: "hidden",
                  }}
                >
                  <table className="eligible-animals-table">
                    <thead>
                      <tr>
                        <th>{copy(pageContract, "label.display_id")}</th>
                        <th>{copy(pageContract, "calendar.drive.shed_header")}</th>
                        <th>
                          {copy(pageContract, "label.animal_identifier_1")}
                        </th>
                        <th>
                          {copy(pageContract, "label.animal_identifier_2")}
                        </th>
                        <th>{copy(pageContract, "label.stage")}</th>
                        <th>{copy(pageContract, "label.lifecycle")}</th>
                        <th>{copy(pageContract, "label.health")}</th>
                        <th>{copy(pageContract, "calendar.drive.reason_header")}</th>
                        <th>{copy(pageContract, "label.status")}</th>
                        <th>{copy(pageContract, "label.when")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {targets.map((row) => (
                        <tr key={row.animal_id}>
                          <td>
                            <span className="gid">{row.display_id}</span>
                          </td>
                          <td>
                            {row.shed_name
                              ? row.operational_location_display ||
                                operationalLocationLabel({
                                  shedName: row.shed_name,
                                  partitionLabel: row.partition_label,
                                  sourceShedName: row.source_shed_name,
                                })
                              : copy(pageContract, "label.placeholder")}
                          </td>
                          <td>
                            {row.animal_identifier_1 ??
                              copy(pageContract, "label.placeholder")}
                          </td>
                          <td>
                            {row.animal_identifier_2 ??
                              copy(pageContract, "label.placeholder")}
                          </td>
                          <td>
                            {row.stage ??
                              copy(pageContract, "label.placeholder")}
                          </td>
                          <td>
                            {row.lifecycle_status ??
                              copy(pageContract, "label.placeholder")}
                          </td>
                          <td>
                            {row.health_status ??
                              copy(pageContract, "label.placeholder")}
                          </td>
                          <td>
                            {driveTargetReason(row) ||
                              copy(pageContract, "label.placeholder")}
                          </td>
                          <td>
                            <Tag
                              tone={
                                optionTone(
                                  pageContract,
                                  "calendar_status",
                                  row.status,
                                ) as
                                  | "ok"
                                  | "warn"
                                  | "dng"
                                  | "info"
                                  | "mut"
                                  | "pur"
                                  | "teal"
                              }
                            >
                              {optionLabel(
                                pageContract,
                                "calendar_status",
                                row.status,
                              )}
                            </Tag>
                          </td>
                          <td className="muted small">
                            {fmtDateTime(row.scheduled_at)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  {(targetsPage > 1 || data.targetsNextCursor) ? (
                    <div className="pager2">
                      <span className="muted small">
                        {copy(pageContract, "schedule.drawer.page_label")} {targetsPage} · {targets.length} {copy(pageContract, "schedule.unit.animals")}
                      </span>
                      <span className="sp" style={{ flex: 1 }} />
                      <button type="button" className="btn sm" disabled={targetsPage <= 1 || targetsLoading} onClick={onPreviousTargets}>
                        <ArrowLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.previous")}
                      </button>
                      <button type="button" className="btn sm" disabled={!data.targetsNextCursor || targetsLoading} onClick={onNextTargets}>
                        {copy(pageContract, "action.next")} <ArrowRight className="ic" style={{ width: 13 }} aria-hidden="true" />
                      </button>
                    </div>
                  ) : null}
                </div>
              ) : (
                <p className="muted small" style={{ margin: 0 }}>
                  {copy(pageContract, "calendar.drawer.eligible_animals_empty")}
                </p>
              )}
            </div>
          ) : null}
          <BlockCard
            title={copy(pageContract, "label.stock_readiness")}
            block={detail.stock}
          />
          <BlockCard
            title={copy(pageContract, "label.proof")}
            block={detail.proof}
          />
          <BlockCard
            title={copy(pageContract, "label.verification")}
            block={detail.verification}
          />

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
                        {[
                          optionLabel(
                            pageContract,
                            "calendar_history_status",
                            h.status,
                          ),
                          h.actor_label,
                          h.channel,
                        ]
                          .filter(Boolean)
                          .join(" · ")}
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
                  <Link
                    key={l.key}
                    href={scopeHref(l.appPath, scope)}
                    className={`tag t-${l.tone}`}
                  >
                    {l.label}
                  </Link>
                ))}
              </div>
            </div>
          ) : null}
        </div>

        {/* Footer — Send nudge / Snooze are real idempotent backend actions; Open drive/workflow deep-links. */}
        <div className="df">
          {!isClosedHistory && !isCatchupSummary ? (
            <>
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
            </>
          ) : null}
          {primaryOpen ? (
            <Link href={primaryOpen.href} className="btn">
              {primaryOpen.label}
            </Link>
          ) : null}
          <button type="button" className="btn" onClick={closeDrawer}>
            {copy(pageContract, "action.close")}
          </button>
        </div>
      </aside>
    </>
  );
}
