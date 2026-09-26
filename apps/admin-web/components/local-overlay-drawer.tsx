"use client";

import Button from "@mui/material/Button";
import type { ReactNode } from "react";
import { DetailDrawer } from "./app/detail-drawer";
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
  const { displayedItem, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items,
    itemId: drawerItemId,
    selectionKey,
    initialSelectedId,
    closeHref,
  });
  if (!displayedItem) return null;

  // Template MinimalDrawer (portal, backdrop, focus return). Escape, Back and the backdrop still go
  // through closeDrawer so the URL/history step stays with useLocalOverlaySelection.
  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={displayedItem.title}
      eyebrow={displayedItem.eyebrow}
      icon={displayedItem.icon}
      ariaLabel={ariaLabel}
      closeLabel={closeLabel}
      footer={
        <>
          {displayedItem.footer}
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>
            {closeLabel}
          </Button>
        </>
      }
    >
      {displayedItem.body}
    </DetailDrawer>
  );
}
