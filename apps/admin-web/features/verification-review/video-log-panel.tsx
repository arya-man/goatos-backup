"use client";

import { LocalOverlayLink, useLocalOverlaySelection } from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import Button from "@mui/material/Button";
import { DetailDrawer } from "@/components/app/detail-drawer";
import { Clock } from "lucide-react";
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
  const { drawerOpen, displayedItem, closeDrawer } = useLocalOverlaySelection({
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
      <Button
        component={LocalOverlayLink}
        href={`#${VIDEO_LOG_PANEL_SELECTION_KEY}=${VIDEO_LOG_PANEL_ID}`}
        replace
        scroll={false}
        // Secondary header action: outlined like Randomization; the header has no primary action (TR1-#35).
        variant="outlined" color="inherit"
        startIcon={<Clock size={18} aria-hidden="true" />}
        className="vr-videolog-btn"
      >
        {copy(pageContract, "video_log.open")}
      </Button>

      {displayedItem ? (
        <DetailDrawer
          open={drawerOpen}
          onClose={closeDrawer}
          title={title}
          subtitle={copy(pageContract, "video_log.hint")}
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
const PANEL_ITEMS = [{ id: VIDEO_LOG_PANEL_ID }] as const;
