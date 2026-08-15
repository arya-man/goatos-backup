"use client";

import { LocalOverlayLink, useLocalOverlaySelection } from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { BarChart3, X } from "lucide-react";
import type { ReactNode } from "react";
import { ANALYTICS_PANEL_ID, ANALYTICS_PANEL_SELECTION_KEY } from "./analytics-panel-params";

/**
 * Opens the CEO/PC-Director oversight analytics in a right-side drawer instead of stacking it above
 * the queue. The queue is the working surface; the analytics are a read the leadership drops into,
 * so the board stays the first thing on screen and the numbers are one click away.
 *
 * The analytics are SERVER-rendered and passed in as `children` -- already in the payload when the
 * page loads, so opening is instant client-local state with no request, no route re-render and no
 * loading wall (apps/admin-web/AGENTS.md -> same-page overlays). The drawer is deliberately NOT
 * gated here: the caller renders this component only when the `oversight_analytics` contract control
 * is enabled, the same capability (permissions.VerificationOversee) that gates the backend endpoint
 * behind `children`. A verifier's page renders neither the trigger nor the panel.
 *
 * `useLocalOverlaySelection` keeps a real `#vi_analytics=open` deep link with Back/Escape/scrim/X
 * close and focus restoration, rather than a bare `useState` toggle that browser Back would ignore.
 */
export function AnalyticsPanel({
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
    selectionKey: ANALYTICS_PANEL_SELECTION_KEY,
    initialSelectedId: initialOpen ? ANALYTICS_PANEL_ID : undefined,
    closeHref,
  });
  const title = copy(pageContract, "oversight_analytics.title");
  const closeLabel = copy(pageContract, "oversight_analytics.close");

  return (
    <>
      {/* Primary-styled on purpose: as a ghost `btn sm` it read as page furniture next to the crumb
          and the maintainer missed it. This is the only entry to the oversight numbers, so it gets
          the page's one primary action. */}
      <LocalOverlayLink href={`#${ANALYTICS_PANEL_SELECTION_KEY}=${ANALYTICS_PANEL_ID}`} className="btn p vr-analytics-btn" replace scroll={false}>
        <BarChart3 className="ic" aria-hidden="true" />
        {copy(pageContract, "oversight_analytics.open")}
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
            className={`drawer vr-analytics-drawer${drawerOpen ? " on" : ""}`}
            aria-label={title}
            aria-hidden={!drawerOpen}
            inert={!drawerOpen}
          >
            <div className="dh">
              <div>
                <h2>{title}</h2>
                <div className="sb">{copy(pageContract, "oversight_analytics.hint")}</div>
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
const PANEL_ITEMS = [{ id: ANALYTICS_PANEL_ID }] as const;
