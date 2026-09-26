"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  notifyLocalOverlayUrlChange,
  pushLocalOverlayUrl,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LeadershipTaskAssignee } from "@/lib/api/server";

import { loadLeadershipTaskAction } from "./actions";
import { TASK_PARAM, type TasksParams } from "./params";
import { TaskDetailDrawer } from "./task-detail-drawer";
import { TaskDetailPanel } from "./task-detail-panel";
import { rowFromTask, type TaskRow } from "./task-row";
import { publishTaskRow, useTaskRowsVersion, taskRowPatch } from "./task-row-store";
import { pickDrawerRow, withPatch } from "./task-detail-pick";

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
  // Bumped on every open (and on Retry): the detail is RE-READ each time the drawer opens, so a
  // task changed elsewhere since the last read never reopens on its old feed and old moves.
  const [readNonce, setReadNonce] = useState(0);
  // The task whose detail read failed, for the inline error + Retry (never a spinner forever).
  const [readFailed, setReadFailed] = useState<string | null>(null);
  // A deep link's first render already carries a fresh detail row; the first open skips the read.
  // Keyed by task + read nonce + staleness, so any later open, Retry or stale-cache re-read (each
  // changes the key) reads again. State, not a ref: it is written during render below.
  const [skipReadKey, setSkipReadKey] = useState<string | null>(initialDetail ? `${initialDetail.id}|0|0` : null);
  const pushedRef = useRef(false);
  useTaskRowsVersion();

  // ---------------------------------------------------------------- open / close
  const open = useCallback((taskID: string, href: string) => {
    setOpenID(taskID);
    setReadFailed(null);
    setReadNonce((n) => n + 1);
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
    if (initialDetail) {
      setSkipReadKey(`${initialDetail.id}|${readNonce}|0`);
      setDetails((current) => ({ ...current, [initialDetail.id]: initialDetail }));
    }
  }

  // Back / forward: the URL is the truth for which task (if any) is open.
  useEffect(() => {
    const onPop = () => {
      const taskID = new URL(window.location.href).searchParams.get(TASK_PARAM.task);
      setOpenID(taskID);
      setReadFailed(null);
      setReadNonce((n) => n + 1);
      pushedRef.current = false;
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  // The list row can move past the cached detail while the drawer is open (someone else moved the
  // task, or the list re-rendered). The patch this drawer published itself is laid over BOTH sides
  // first, so our own comment / status change never reads as "someone else changed it"; only a
  // list row still newer after that means the cached feed is stale, and it is read again: the
  // stale version is part of the read effect's key (derived, not a nonce bumped from an effect).
  const openSummaryVersion = openID ? rows.find((row) => row.id === openID)?.rowVersion ?? 0 : 0;
  const openPatch = openID ? taskRowPatch(openID) : undefined;
  const openCachedVersion = openID && details[openID] ? withPatch(details[openID], openPatch).rowVersion : 0;
  const cacheBehindList = !preview && openCachedVersion > 0 && openSummaryVersion > openCachedVersion;
  const staleReadKey = cacheBehindList ? openSummaryVersion : 0;

  // ---------------------------------------------------------------- the detail read
  useEffect(() => {
    if (!openID || preview) return;
    if (skipReadKey === `${openID}|${readNonce}|${staleReadKey}`) return;
    let cancelled = false;
    // A plain promise with its own catch, NOT a transition: a rejected server action inside
    // startTransition reaches the route's error boundary and takes the page down (2026-09-25).
    loadLeadershipTaskAction(openID)
      .then((result) => {
        if (cancelled) return;
        if (!result.ok) {
          setReadFailed(openID);
          return;
        }
        const row = rowFromTask(result.task);
        setDetails((current) => ({ ...current, [openID]: row }));
        // The WHOLE fresh row, not just its version: a version-only patch kept an older optimistic
        // status on top of the newer row.
        publishTaskRow(openID, row);
      })
      .catch(() => {
        if (!cancelled) setReadFailed(openID);
      });
    return () => {
      cancelled = true;
    };
  }, [openID, readNonce, preview, staleReadKey, skipReadKey]);

  const retryRead = useCallback(() => {
    setReadFailed(null);
    setReadNonce((n) => n + 1);
  }, []);


  if (!openID) return null;

  const summary = rows.find((row) => row.id === openID) ?? null;
  // The published patch (a status changed here, a version bumped by a comment) is laid over the
  // cached detail AND the list row before they are compared, so this drawer's own write keeps its
  // feed; then the newer of the two wins (see `pickDrawerRow`).
  const patch = taskRowPatch(openID);
  const cached = details[openID];
  const picked = pickDrawerRow(cached ? withPatch(cached, patch) : undefined, summary ? withPatch(summary, patch) : null);
  if (!picked.row) return null;
  const detail = picked.row;
  // Preview fixture rows are complete and never read live, so they are never "loading".
  const detailLoaded = picked.detailLoaded || preview;
  const failed = readFailed === openID;

  return (
    <TaskDetailDrawer taskId={openID} title={detail.number} onClose={close} ariaLabel={ariaLabel} closeLabel={closeLabel}>
      <TaskDetailPanel
        detail={detail}
        pageContract={pageContract}
        scopeKey={scopeKey}
        params={params}
        assignees={assignees}
        canRaise={canRaise}
        onClose={close}
        loadingDetail={!detailLoaded && !failed}
        detailLoaded={detailLoaded}
        detailError={
          failed && !detailLoaded
            ? copy(pageContract, "activity.load_failed", "Activity could not be loaded.")
            : undefined
        }
        onRetryDetail={failed ? retryRead : undefined}
      />
    </TaskDetailDrawer>
  );
}
