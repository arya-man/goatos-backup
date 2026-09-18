"use client";

import { useCallback, useEffect, useRef, useState, useTransition } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  notifyLocalOverlayUrlChange,
  pushLocalOverlayUrl,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LeadershipTaskAssignee } from "@/lib/api/server";

import { loadLeadershipTaskAction } from "./actions";
import { TASK_PARAM, type TasksParams } from "./params";
import { TaskDetailDrawer } from "./task-detail-drawer";
import { TaskDetailPanel } from "./task-detail-panel";
import { rowFromTask, type TaskRow } from "./task-row";
import { publishTaskRow, useTaskRowsVersion, taskRowPatch } from "./task-row-store";

/**
 * The task drawer as CLIENT-LOCAL state -- the repo's rule for a same-page overlay.
 *
 * Before this, a card click was a route navigation: the server re-rendered the whole page
 * (~0.9 s on the CEO's machine) and the route's loading skeleton flashed, to open a drawer for a
 * row that was already on screen. Now:
 *
 *   - a click on a card or a table row is intercepted here (delegated, so the card and the row
 *     components keep their real hrefs for no-JS and for middle-click), the drawer opens at once
 *     from the row already in `rows`, and the URL gains `task=` through history only;
 *   - the row's notes and activity -- which the list deliberately does not carry -- are fetched
 *     inside the drawer by one server action, with a one-line loading state in the feed;
 *   - Close / Escape / scrim pop the history entry (or replace the URL) and never navigate;
 *   - a deep link (`?task=` on first load) still works: the server hands the detail row in
 *     `initialDetail`, and this host starts open with it.
 *
 * A status change or a comment made in the drawer publishes to the row store; the board and the
 * table read that store, so the card moves and the pill updates without a route render.
 */
export function TaskDrawerHost({
  rows,
  initialDetail,
  pageContract,
  scopeKey,
  params,
  assignees,
  canRaise,
  ariaLabel,
  closeLabel,
  closeHref,
  preview = false,
}: {
  rows: TaskRow[];
  initialDetail: TaskRow | null;
  pageContract: AdminUiPageContract;
  scopeKey: string;
  params: TasksParams;
  assignees: LeadershipTaskAssignee[];
  canRaise: boolean;
  ariaLabel: string;
  closeLabel: string;
  /** The page's URL with `task=` dropped: what the address bar shows once the drawer is closed. */
  closeHref: string;
  /** /tasks-preview: fixture rows are complete and read-only; nothing here may call a live action. */
  preview?: boolean;
}) {
  const [openID, setOpenID] = useState<string | null>(initialDetail?.id ?? params.selectedTaskID ?? null);
  // Detail rows (with notes + activity) by task id, from the deep-link render or a fetch here.
  const [details, setDetails] = useState<Record<string, TaskRow>>(() =>
    initialDetail ? { [initialDetail.id]: initialDetail } : {},
  );
  const [loadingID, setLoadingID] = useState<string | null>(null);
  const [, startTransition] = useTransition();
  const pushedRef = useRef(false);
  useTaskRowsVersion();

  // ---------------------------------------------------------------- open / close
  const open = useCallback((taskID: string, href: string) => {
    setOpenID(taskID);
    pushedRef.current = pushLocalOverlayUrl(href);
    notifyLocalOverlayUrlChange();
  }, []);

  const close = useCallback(() => {
    setOpenID(null);
    if (pushedRef.current && currentHistoryEntryIsLocalOverlay()) {
      pushedRef.current = false;
      window.history.back();
    } else {
      replaceLocalOverlayUrl(closeHref);
    }
    notifyLocalOverlayUrlChange();
  }, [closeHref]);

  // Delegated click: any card or table row link on the page opens the drawer here.
  useEffect(() => {
    const onClick = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0) return;
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      const link = target?.closest?.("a.ltb-card, a.lt-tasklink") as HTMLAnchorElement | null;
      if (!link) return;
      const url = new URL(link.href, window.location.href);
      const taskID = url.searchParams.get(TASK_PARAM.task);
      if (!taskID) return;
      event.preventDefault();
      open(taskID, url.pathname + url.search + url.hash);
    };
    // CAPTURE phase: Next's <Link> handles the click at React's root and would navigate before a
    // bubbling listener ever ran; intercepting on the way down lets preventDefault stop it.
    document.addEventListener("click", onClick, true);
    return () => document.removeEventListener("click", onClick, true);
  }, [open]);

  // The server can still change the selection (a deep link, a full render after a form post):
  // follow the prop, so the host never disagrees with the URL it was rendered for.
  const serverSelected = initialDetail?.id ?? params.selectedTaskID ?? null;
  const [seenServerSelected, setSeenServerSelected] = useState(serverSelected);
  if (seenServerSelected !== serverSelected) {
    // Derived-state form: adjusted during render, not in an effect.
    setSeenServerSelected(serverSelected);
    setOpenID(serverSelected);
    if (initialDetail) setDetails((current) => ({ ...current, [initialDetail.id]: initialDetail }));
  }

  // Back / forward: the URL is the truth for which task (if any) is open.
  useEffect(() => {
    const onPop = () => {
      const taskID = new URL(window.location.href).searchParams.get(TASK_PARAM.task);
      setOpenID(taskID);
      pushedRef.current = false;
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  // ---------------------------------------------------------------- the detail read
  useEffect(() => {
    if (!openID || details[openID] || preview) return;
    let cancelled = false;
    startTransition(async () => {
      setLoadingID(openID);
      const result = await loadLeadershipTaskAction(openID);
      if (cancelled) return;
      if (result.ok) {
        const row = rowFromTask(result.task);
        setDetails((current) => ({ ...current, [openID]: row }));
        publishTaskRow(openID, { rowVersion: row.rowVersion });
      }
      setLoadingID((current) => (current === openID ? null : current));
    });
    return () => {
      cancelled = true;
    };
  }, [openID, details, preview]);

  if (!openID) return null;

  const summary = rows.find((row) => row.id === openID) ?? null;
  const base = details[openID] ?? summary;
  if (!base) return null;
  // The published patch (a status changed here, a version bumped by a comment) on top.
  const patch = taskRowPatch(openID);
  const detail =
    patch && typeof patch.rowVersion === "number" && patch.rowVersion > base.rowVersion
      ? { ...base, ...patch }
      : base;

  return (
    <TaskDetailDrawer taskId={openID} onClose={close} ariaLabel={ariaLabel} closeLabel={closeLabel}>
      <TaskDetailPanel
        detail={detail}
        pageContract={pageContract}
        scopeKey={scopeKey}
        params={params}
        assignees={assignees}
        canRaise={canRaise}
        onClose={close}
        loadingDetail={loadingID === openID && !details[openID]}
      />
    </TaskDetailDrawer>
  );
}
