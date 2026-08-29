"use client";

import { LocalOverlayLink, useLocalOverlaySelection } from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Shuffle, X } from "lucide-react";
import type { ReactNode } from "react";
import { RANDOMIZATION_PANEL_ID, RANDOMIZATION_PANEL_SELECTION_KEY } from "./randomization-panel-params";

/**
 * Opens the CEO-only RANDOMIZATION section in a right-side drawer rather than stacking it above the
 * queue, for the same reason the analytics panel does: the queue is the working surface, and this
 * is a setting leadership drops into occasionally, not a board anyone watches.
 *
 * The rows are SERVER-rendered and passed in as `children` — already in the payload when the page
 * loads, so opening is instant client-local state with no request and no loading wall
 * (apps/admin-web/AGENTS.md -> same-page overlays). The drawer is deliberately NOT gated here: the
 * caller renders this component only when the `randomization` contract control is enabled, the same
 * capability (permissions.VerificationSampling) that gates the endpoint behind `children`. A
 * verifier — and a director — renders neither the trigger nor the panel.
 */
export function RandomizationPanel({
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
    selectionKey: RANDOMIZATION_PANEL_SELECTION_KEY,
    initialSelectedId: initialOpen ? RANDOMIZATION_PANEL_ID : undefined,
    closeHref,
  });
  const title = copy(pageContract, "randomization.title");
  const closeLabel = copy(pageContract, "randomization.close");

  return (
    <>
      <LocalOverlayLink
        href={`#${RANDOMIZATION_PANEL_SELECTION_KEY}=${RANDOMIZATION_PANEL_ID}`}
        className="btn sm"
        replace
        scroll={false}
      >
        <Shuffle className="ic" aria-hidden="true" />
        {copy(pageContract, "randomization.open")}
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
                <div className="sb">{copy(pageContract, "randomization.hint")}</div>
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
const PANEL_ITEMS = [{ id: RANDOMIZATION_PANEL_ID }] as const;
