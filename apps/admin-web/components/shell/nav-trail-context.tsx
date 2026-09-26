"use client";

import { createContext, useContext } from "react";

export type NavTrailItem = { label: string; href: string };

export type NavTrail = {
  /** Pages the reader came through in this session, oldest first. Empty on a sidebar entry. */
  items: NavTrailItem[];
  /** Step back one page (history back), dropping the last trail entry. */
  back: () => void;
  /** Bottom-bar label for the back link, e.g. "Back to Control Tower". */
  backTitle: (item: NavTrailItem) => string;
};

const EMPTY: NavTrail = { items: [], back: () => undefined, backTitle: (item) => item.label };

/**
 * The shell's session trail, read by the page header. The shell used to render the trail as its own
 * "Back" bar above every page, so a drilled-in page carried two headers. The trail now only decides
 * where the page header's back link (the template's `CustomBreadcrumbs` heading) goes.
 */
export const NavTrailContext = createContext<NavTrail>(EMPTY);

export function useNavTrail(): NavTrail {
  return useContext(NavTrailContext);
}
