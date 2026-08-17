"use client";

import { LocalOverlayLink, useLocalOverlaySelection } from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Clock, X } from "lucide-react";
import type { ReactNode } from "react";
import { VIDEO_LOG_PANEL_ID, VIDEO_LOG_PANEL_SELECTION_KEY } from "./video-log-params";

/**
 * Opens the VIDEO LOG in a right-side drawer: for one business day, per shed, the time each proof
 * arrived (maintainer decision 2026-08-14).
 *
 * Sibling of AnalyticsPanel and deliberately a SECOND panel rather than a tab inside it: the two
 * answer different questions for different people. Analytics is the leadership backlog aggregate
 * behind permissions.VerificationOversee; this is the day's arrival record behind
 * permissions.VerificationEvidenceTimeline, which the VERIFIER holds and oversight is not. Folding
 * one into the other would have tied the verifier's access to the capability that was deliberately
 * withheld from her in the 2026-08-12 STG incident.
 *
 * The content is SERVER-rendered and passed in as `children` -- already in the payload when the
 * page loads, so opening is instant client-local state with no request and no loading wall
 * (apps/admin-web/AGENTS.md -> same-page overlays). Not gated here: the caller renders this only
 * when the `video_log` contract control is enabled, the same capability that gates the endpoint
 * behind `children`.
 *
 * `useLocalOverlaySelection` keeps a real `#vi_video_log=open` deep link with Back/Escape/scrim/X
 * close and focus restoration, rather than a bare `useState` toggle that browser Back would ignore.
 */
export function VideoLogPanel({
  pageContract,
  closeHref,
  initialOpen,
  children,
}: {
  pageContract: AdminUiPageContract;
  /** URL to restore when the drawer closes without a history entry to pop (a direct deep link). */
  closeHref: string;
  /** True when the incoming URL already asked for the panel, so a shared link opens it. */
  initialOpen?: boolean;
  children: ReactNode;
}) {
  const { drawerOpen, displayedItem, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: PANEL_ITEMS,
    itemId: (item) => item.id,
    selectionKey: VIDEO_LOG_PANEL_SELECTION_KEY,
    initialSelectedId: initialOpen ? VIDEO_LOG_PANEL_ID : undefined,
    closeHref,
  });
  const title = copy(pageContract, "video_log.title");
  const closeLabel = copy(pageContract, "video_log.close");

  return (
    <>
      {/* Matched to Analytics: same primary treatment, same size, sitting beside it (maintainer,
          2026-08-15). These are two peer entry points into the same evidence — one to the backlog
          numbers, one to the day's arrivals — so ranking one above the other by weight made the
          Video Log read as secondary chrome rather than the sibling it is. Both share ONE style
          rule in mesha-theme.css; do not give either its own size or weight. */}
      <LocalOverlayLink
        href={`#${VIDEO_LOG_PANEL_SELECTION_KEY}=${VIDEO_LOG_PANEL_ID}`}
        className="btn p vr-videolog-btn"
        replace
        scroll={false}
      >
        <Clock className="ic" aria-hidden="true" />
        {copy(pageContract, "video_log.open")}
      </LocalOverlayLink>

      {displayedItem ? (
        <>
          <button
            type="button"
            className={`scrim${drawerOpen ? " on" : ""}`}
            aria-label={closeLabel}
            aria-hidden={!drawerOpen}
            tabIndex={drawerOpen ? 0 : -1}
            onClick={closeDrawer}
          />
          <aside
            className={`drawer vr-videolog-drawer${drawerOpen ? " on" : ""}`}
            aria-label={title}
            aria-hidden={!drawerOpen}
            inert={!drawerOpen}
          >
            <div className="dh">
              <div>
                <h2>{title}</h2>
                <div className="sb">{copy(pageContract, "video_log.hint")}</div>
              </div>
              <span className="sp" style={{ flex: 1 }} />
              <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={closeLabel} onClick={closeDrawer}>
                <X className="ic" />
              </button>
            </div>
            <div className="dc">{children}</div>
          </aside>
        </>
      ) : null}
    </>
  );
}

// One synthetic item: the hook is built for a selected RECORD out of a list, and this panel is the
// degenerate one-record case. Kept module-level so the array identity is stable across renders --
// the hook's effect depends on `items`, and a fresh array each render would re-subscribe forever.
const PANEL_ITEMS = [{ id: VIDEO_LOG_PANEL_ID }] as const;
