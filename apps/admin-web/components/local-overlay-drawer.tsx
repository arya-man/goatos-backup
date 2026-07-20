"use client";

import { X } from "lucide-react";
import type { ReactNode } from "react";
import { useLocalOverlaySelection } from "./local-overlay-link";

export type LocalOverlayDrawerItem = {
  id: string;
  eyebrow: ReactNode;
  title: ReactNode;
  icon?: ReactNode;
  body: ReactNode;
  footer?: ReactNode;
};

function drawerItemId(item: LocalOverlayDrawerItem): string {
  return item.id;
}

/**
 * Client-owned drawer shell for server-rendered list data. Server Components
 * may pass rich React-node slots, while selection, animation, focus, Escape,
 * outside-click, and browser history remain entirely local after hydration.
 */
export function LocalOverlayDrawer({
  items,
  selectionKey,
  initialSelectedId,
  closeHref,
  ariaLabel,
  closeLabel,
}: {
  items: LocalOverlayDrawerItem[];
  selectionKey: string;
  initialSelectedId?: string;
  closeHref: string;
  ariaLabel: string;
  closeLabel: string;
}) {
  const { displayedItem, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items,
    itemId: drawerItemId,
    selectionKey,
    initialSelectedId,
    closeHref,
  });
  if (!displayedItem) return null;

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label={closeLabel}
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <aside className={`drawer${drawerOpen ? " on" : ""}`} aria-label={ariaLabel} aria-hidden={!drawerOpen} inert={!drawerOpen}>
        <div className="dh">
          {displayedItem.icon ? (
            <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
              {displayedItem.icon}
            </span>
          ) : null}
          <div>
            <div className="mt">{displayedItem.eyebrow}</div>
            <h2>{displayedItem.title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={closeLabel} onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">{displayedItem.body}</div>
        <div className="df">
          {displayedItem.footer}
          <button type="button" className="btn" onClick={closeDrawer}>
            {closeLabel}
          </button>
        </div>
      </aside>
    </>
  );
}
