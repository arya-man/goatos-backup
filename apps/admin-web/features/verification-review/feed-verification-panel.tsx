"use client";

import { LocalOverlayLink, useLocalOverlaySelection } from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Scale, X } from "lucide-react";
import type { ReactNode } from "react";

/**
 * Opens FEED VERIFICATION in a drawer beside the Video Log (maintainer decision 2026-09-28): for one
 * feed day, per park, pen and session, the planned feed beside the weight the verifier entered for
 * the bag packed the day before.
 *
 * A sibling of VideoLogPanel, same mechanics: the content is SERVER-rendered and passed in as
 * `children`, so opening is instant client-local state with no request; `useLocalOverlaySelection`
 * keeps a real `#vi_feed_verify=open` deep link with Back/Escape/scrim/X close and focus
 * restoration. Not gated here: the caller renders this only when the `feed_verification` contract
 * control is enabled -- the same capability (permissions.VerificationFeedPackingLog, verifier + CXO)
 * that gates the endpoint behind `children`.
 *
 * Keys arrive as props, not imports: a constant imported into a "use client" module from the
 * server page is fine, but this panel must not become the module the page imports them FROM (see
 * video-log-params.ts), so the page passes them in from feed-verification-params.ts.
 */
export function FeedVerificationPanel({
  pageContract,
  closeHref,
  initialOpen,
  selectionKey,
  panelId,
  children,
}: {
  pageContract: AdminUiPageContract;
  /** URL to restore when the drawer closes without a history entry to pop (a direct deep link). */
  closeHref: string;
  /** True when the incoming URL already asked for the panel, so a shared link opens it. */
  initialOpen?: boolean;
  selectionKey: string;
  panelId: string;
  children: ReactNode;
}) {
  const { drawerOpen, displayedItem, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: PANEL_ITEMS,
    itemId: (item) => item.id,
    selectionKey,
    initialSelectedId: initialOpen ? panelId : undefined,
    closeHref,
  });
  const title = copy(pageContract, "feed_verification.title");
  const closeLabel = copy(pageContract, "feed_verification.close");

  return (
    <>
      {/* Same treatment as the Video Log button beside it: peer entry points share ONE style rule
          (vr-videolog-btn in mesha-theme.css). */}
      <LocalOverlayLink href={`#${selectionKey}=${panelId}`} className="btn p vr-videolog-btn" replace scroll={false}>
        <Scale className="ic" aria-hidden="true" />
        {copy(pageContract, "feed_verification.open")}
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
            className={`drawer vr-feedverify-drawer${drawerOpen ? " on" : ""}`}
            aria-label={title}
            aria-hidden={!drawerOpen}
            inert={!drawerOpen}
          >
            <div className="dh">
              <div>
                <h2>{title}</h2>
                <div className="sb">{copy(pageContract, "feed_verification.hint")}</div>
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

// One synthetic item, module-level so its identity is stable across renders (see VideoLogPanel).
const PANEL_ITEMS = [{ id: "open" }] as const;
