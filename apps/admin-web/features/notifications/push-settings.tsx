"use client";

/**
 * What the notification centre's ⚙ opens -- and the ONLY thing it does: browser push.
 *
 * Three real backend controls exist (lib/api/browser-push-server.ts): register this browser,
 * unregister a browser, list the caller's registered browsers. So this section is exactly that:
 *   * a switch "Push on this browser" -- on = the existing web-push flow (`enableWebPush`:
 *     permission prompt, FCM token, register), off = `disableWebPush` (drop the token,
 *     unregister); disabled with the browser's own reason when it cannot work here;
 *   * the list of registered browsers (label · registered date · status), each with Remove
 *     (`unregisterBrowserPush` by that row's install id; this browser's row reads "This browser").
 *
 * Nothing here prompts on mount. Chrome auto-denies load-time permission requests and applies
 * its quiet UI to the origin for good; the prompt is behind the switch. What does happen on
 * mount is the silent half for a browser that already granted: its token is re-read and
 * re-registered, because Chrome rotates tokens and tells the server nothing.
 *
 * Loaded lazily by the panel the first time ⚙ is pressed, so `firebase/messaging` is paid for
 * only by a person who opens this section.
 */

import { useCallback, useEffect, useState } from "react";
import { Iconify } from "@/components/minimal/iconify";
import CircularProgress from "@mui/material/CircularProgress";
import { fmtDateTime } from "@/lib/format";
import { pushCopy } from "@/lib/push-copy";
import type { BrowserPushRegistration } from "@/lib/api/browser-push-server";
import { listBrowserPushRegistrations, unregisterBrowserPush } from "@/lib/web-push-actions";
import {
  detectWebPushSupport,
  disableWebPush,
  enableWebPush,
  getBrowserInstallId,
  readNotificationPermission,
  refreshWebPushRegistration,
  resolveWebPushState,
  type WebPushState,
} from "@/lib/web-push";
import { cx } from "@/lib/tone";
import type { NotificationCentreCopy } from "./notification-copy";

type Busy = "idle" | "checking" | "enabling" | "disabling" | "listing" | "removing";

export function PushSettings({ centreCopy, contractCopy }: { centreCopy: NotificationCentreCopy; contractCopy?: Record<string, string> }) {
  const copy = useCallback((key: string) => pushCopy(contractCopy, key), [contractCopy]);
  const [state, setState] = useState<WebPushState>({ status: "unsupported", reason: "" });
  const [busy, setBusy] = useState<Busy>("checking");
  const [note, setNote] = useState("");
  const [rows, setRows] = useState<BrowserPushRegistration[] | null>(null);
  const [listError, setListError] = useState("");
  const [removing, setRemoving] = useState<string | null>(null);
  const [installId, setInstallId] = useState("");

  const loadList = useCallback(async () => {
    try {
      const result = await listBrowserPushRegistrations();
      if (!result.ok) {
        setListError(centreCopy.error);
        return;
      }
      setListError("");
      setRows(result.data.registrations.filter((row) => row.status !== "unsubscribed"));
    } catch {
      setListError(centreCopy.error);
    }
  }, [centreCopy.error]);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const support = detectWebPushSupport();
      const permission = readNotificationPermission();
      const id = support.supported ? getBrowserInstallId() : "";
      const initial = resolveWebPushState({
        supported: support.supported,
        supportReason: support.reason,
        configured: true,
        permission,
        hasRegistration: false,
        browserInstallId: id,
      });
      if (cancelled) return;
      setInstallId(id);
      setState(initial);
      // The silent half for an already-granted browser: re-read + re-register the token.
      const next = support.supported && permission === "granted" ? await refreshWebPushRegistration() : initial;
      if (cancelled) return;
      setState(next);
      setBusy("idle");
      await loadList();
    })();
    return () => {
      cancelled = true;
    };
  }, [loadList]);

  // ON = this browser has granted permission AND its install id is in the registration list
  // (an `enabled` state from the flow counts until the list has answered). Never the flow's
  // word alone: a browser that granted last month but whose row went stale reads OFF.
  const registeredHere = rows === null ? state.status === "enabled" : rows.some((row) => row.browser_install_id === installId && row.status === "active");
  const enabled = readNotificationPermission() === "granted" && registeredHere;
  const blocked = state.status === "blocked" || state.status === "unsupported" || state.status === "unconfigured";
  const pending = busy !== "idle";

  const toggle = useCallback(async () => {
    setNote("");
    if (enabled) {
      setBusy("disabling");
      try {
        setState(await disableWebPush());
      } finally {
        setBusy("idle");
      }
    } else {
      setBusy("enabling");
      try {
        const next = await enableWebPush();
        setState(next);
        // Only OUR copy reaches the screen: the flow's `reason` strings are diagnostics.
        if (next.status === "dismissed") setNote(copy("push.dismissed"));
        if (next.status === "timed_out" || next.status === "error") setNote(centreCopy.pushFailed);
      } finally {
        setBusy("idle");
      }
    }
    await loadList();
  }, [centreCopy.pushFailed, copy, enabled, loadList]);

  const remove = useCallback(
    async (row: BrowserPushRegistration) => {
      setRemoving(row.browser_registration_id);
      try {
        const result = await unregisterBrowserPush({ browserInstallId: row.browser_install_id });
        if (!result.ok) {
          setListError(centreCopy.pushFailed);
          return;
        }
        if (row.browser_install_id === installId) setState({ status: "prompt" });
        await loadList();
      } finally {
        setRemoving(null);
      }
    },
    [centreCopy.pushFailed, installId, loadList],
  );

  // The section is only rendered when the stack and the browser can push, so "unsupported" /
  // "unconfigured" never carry a sentence here; "blocked" is the one state a person can fix.
  const reason = state.status === "blocked" ? copy("push.blocked") : "";

  return (
    <div className="nc-push" data-push-settings>
      <div className="nc-push-row">
        <span className="nc-push-text">
          <span className="nc-push-label">{centreCopy.pushThisBrowser}</span>
          <span className="nc-push-hint">{reason || note || copy("push.this_browser_only")}</span>
        </span>
        <label className={cx("nc-switch", enabled && "on", (pending || blocked) && "is-disabled")}>
          <input
            type="checkbox"
            role="switch"
            className="nc-switch-input"
            checked={enabled}
            aria-label={centreCopy.pushThisBrowser}
            disabled={pending || blocked}
            onChange={() => void toggle()}
          />
          {busy === "enabling" || busy === "disabling" || busy === "checking" ? <CircularProgress size={16} color="inherit" className="nc-spin" /> : null}
          <span className="nc-switch-knob" aria-hidden="true" />
        </label>
      </div>

      <div className="nc-push-list" aria-busy={rows === null}>
        <span className="nc-push-listhead">{centreCopy.pushBrowsers}</span>
        {listError ? <span className="nc-push-err">{listError}</span> : null}
        {rows === null && !listError ? <span className="nc-push-hint">{centreCopy.busy}</span> : null}
        {rows !== null && rows.length === 0 && !listError ? <span className="nc-push-hint">{centreCopy.pushNoBrowsers}</span> : null}
        {rows?.map((row) => {
          const mine = row.browser_install_id === installId;
          return (
            <div key={row.browser_registration_id} className={cx("nc-push-item", row.status === "stale" && "nc-push-stale")}>
              <Iconify icon="solar:monitor-bold" />
              <span className="nc-push-itemtext">
                <span className="nc-push-itemlabel">
                  {row.browser_label || copy("push.enabled")}
                  {mine ? <span className="nc-chip">{centreCopy.pushThisOne}</span> : null}
                  {row.status === "stale" ? <span className="nc-chip">{centreCopy.pushStale}</span> : null}
                </span>
                <span className="nc-push-hint">{fmtDateTime(row.created_at)}</span>
              </span>
              <button
                type="button"
                className="nc-btn nc-btn-outlined nc-btn-xs"
                disabled={removing !== null || pending}
                onClick={() => void remove(row)}
              >
                {removing === row.browser_registration_id ? <CircularProgress size={16} color="inherit" className="nc-spin" /> : null}
                {centreCopy.pushRemove}
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
