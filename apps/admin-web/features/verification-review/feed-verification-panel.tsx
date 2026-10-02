"use client";

import { LocalOverlayLink, useLocalOverlaySelection } from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Scale, X } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

import { loadFeedVerificationLogAction } from "./feed-verification-actions";
import { FeedVerificationView, type FeedVerificationViewProps } from "./feed-verification-view";

/**
 * Opens FEED VERIFICATION in a drawer beside the Video Log (maintainer decision 2026-09-28): for one
 * feed day, per park, pen and session, the planned feed beside the weight the verifier entered for
 * the bag packed the day before.
 *
 * A sibling of VideoLogPanel for opening and closing: instant client-local state with no request;
 * `useLocalOverlaySelection`
 * keeps a real `#vi_feed_verify=open` deep link with Back/Escape/scrim/X close and focus
 * restoration. Not gated here: the caller renders this only when the `feed_verification` contract
 * control is enabled -- the same capability (permissions.VerificationFeedPackingLog, verifier + CXO)
 * that gates the endpoint behind `children`.
 *
 * THE DAY IS NOT FETCHED BY THE PAGE UNLESS THE URL ASKS FOR THE PANEL. `children` is the
 * server-rendered day only when the query already says open (a deep link, or a date / park change
 * made inside the panel). Otherwise it is absent, and the drawer loads the day ITSELF the first
 * time it opens (loadFeedVerificationLogAction) -- so a /verify render with the panel closed, which
 * is every queue landing and every Accept redirect to the next video, makes no feed read at all.
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
  view,
  feedDay,
  parkId,
  children,
}: {
  pageContract: AdminUiPageContract;
  /** URL to restore when the drawer closes without a history entry to pop (a direct deep link). */
  closeHref: string;
  /** True when the incoming URL already asked for the panel, so a shared link opens it. */
  initialOpen?: boolean;
  selectionKey: string;
  panelId: string;
  /** How to render a day the drawer loaded itself. */
  view: FeedVerificationViewProps;
  /** The day and top-bar park scope the drawer loads when it has no server-rendered day. */
  feedDay?: string;
  parkId?: string;
  /** The server-rendered day, present only when the URL already asked for the panel. */
  children?: ReactNode;
}) {
  const { drawerOpen, displayedItem, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: PANEL_ITEMS,
    itemId: (item) => item.id,
    selectionKey,
    initialSelectedId: initialOpen ? panelId : undefined,
    closeHref,
  });
  const serverRendered = children != null;
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
            <div className="dc">
              {serverRendered ? (
                children
              ) : drawerOpen ? (
                <FeedVerificationClientBody pageContract={pageContract} view={view} feedDay={feedDay} parkId={parkId} />
              ) : (
                <FeedVerificationSkeleton />
              )}
            </div>
          </aside>
        </>
      ) : null}
    </>
  );
}

// One synthetic item, module-level so its identity is stable across renders (see VideoLogPanel).
const PANEL_ITEMS = [{ id: "open" }] as const;

function FeedVerificationClientBody({
  pageContract,
  view,
  feedDay,
  parkId,
}: {
  pageContract: AdminUiPageContract;
  view: FeedVerificationViewProps;
  feedDay?: string;
  parkId?: string;
}) {
  const [loaded, setLoaded] = useState<
    { state: "loading" } | { state: "ready"; log: Parameters<typeof FeedVerificationView>[0]["log"] } | { state: "failed" }
  >({ state: "loading" });

  // This component only mounts while the drawer is open. Closing the drawer unmounts it and forgets
  // the loaded day, so a later open reads fresh totals without fetching on the page render path.
  useEffect(() => {
    let live = true;
    loadFeedVerificationLogAction(feedDay, parkId)
      .then((result) => {
        if (live) setLoaded(result.ok ? { state: "ready", log: result.log } : { state: "failed" });
      })
      .catch(() => {
        if (live) setLoaded({ state: "failed" });
      });
    return () => {
      live = false;
    };
  }, [feedDay, parkId]);

  if (loaded.state === "ready") return <FeedVerificationView {...view} log={loaded.log} />;
  if (loaded.state === "failed") return <div className="small muted">{copy(pageContract, "feed_verification.unavailable")}</div>;
  return <FeedVerificationSkeleton />;
}

function FeedVerificationSkeleton() {
  return (
    <div className="vr-feedverify" aria-busy="true">
      <div className="skel" style={{ width: 220, height: 32 }} />
      <div className="skel" style={{ width: "100%", height: 56, marginTop: 12 }} />
      <div className="skel" style={{ width: "100%", height: 180, marginTop: 12 }} />
    </div>
  );
}
