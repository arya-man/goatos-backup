"use client";

import { LocalOverlayLink, useLocalOverlaySelection } from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import Button from "@mui/material/Button";
import { DetailDrawer } from "@/components/app/detail-drawer";
import { Iconify } from "@/components/minimal/iconify";
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
  const { drawerOpen, displayedItem, closeDrawer } = useLocalOverlaySelection({
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
      <Button
        component={LocalOverlayLink}
        href={`#${ANALYTICS_PANEL_SELECTION_KEY}=${ANALYTICS_PANEL_ID}`}
        replace
        scroll={false}
        // Secondary header action: outlined like Randomization; the header has no primary action (TR1-#35).
        variant="outlined" color="inherit"
        startIcon={<Iconify icon="solar:chart-square-outline" width={18} aria-hidden="true" />}
        className="vr-analytics-btn"
      >
        {copy(pageContract, "oversight_analytics.open")}
      </Button>

      {displayedItem ? (
        <DetailDrawer
          open={drawerOpen}
          onClose={closeDrawer}
          title={title}
          subtitle={copy(pageContract, "oversight_analytics.hint")}
          ariaLabel={title}
          closeLabel={closeLabel}
        >
          {children}
        </DetailDrawer>
      ) : null}
    </>
  );
}

// One synthetic item: the hook is built for a selected RECORD out of a list, and this panel is the
// degenerate one-record case. Kept module-level so the array identity is stable across renders --
// the hook's effect depends on `items`, and a fresh array each render would re-subscribe forever.
const PANEL_ITEMS = [{ id: ANALYTICS_PANEL_ID }] as const;
