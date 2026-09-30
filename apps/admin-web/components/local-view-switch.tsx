"use client";

// A view toggle that NEVER asks the server again (maintainer rule 2026-09-24: "no toggle should
// reload the whole page"). Every view it switches between is already rendered on the page, so a
// click only changes which one is visible and rewrites the URL in place -- the same
// replaceLocalOverlayUrl drawers use -- so the choice still survives a reload and pastes as a link.
//
// Two pieces, because the toggle and the views it controls sit in different parts of a page:
// LocalViewToggle is the segmented control, LocalViewPane wraps each view. They meet on the URL
// parameter name and a window event, never on a shared parent.
//
// It renders NO copy of its own: labels arrive already resolved from the page contract.
import { useEffect, useState, type ReactNode } from "react";
import Box from "@mui/material/Box";
import { replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { SegmentTabs } from "@/components/app/list/segment-tabs";

const CHANGE_EVENT = "mesha:local-view-change";

type ChangeDetail = { param: string; value: string };

export type LocalViewOption = { value: string; label: string };

function useLocalView(param: string, initial: string): [string, (value: string) => void] {
  const [value, setValue] = useState(initial);
  useEffect(() => {
    const onChange = (event: Event) => {
      const detail = (event as CustomEvent<ChangeDetail>).detail;
      if (detail?.param === param) setValue(detail.value);
    };
    window.addEventListener(CHANGE_EVENT, onChange);
    return () => window.removeEventListener(CHANGE_EVENT, onChange);
  }, [param]);
  return [value, setValue];
}

export function LocalViewToggle({
  param,
  current,
  defaultValue,
  options,
  ariaLabel,
}: {
  /** The URL parameter that remembers the choice. */
  param: string;
  /** The view the server rendered as visible. */
  current: string;
  /** The value that leaves the parameter out of the URL. */
  defaultValue: string;
  options: readonly LocalViewOption[];
  /** Already resolved from the page contract by the caller. */
  ariaLabel?: string;
}) {
  const [selected, setSelected] = useLocalView(param, current);
  return (
    // The wrapper tags every copy of this toggle (each pane may carry its own) so a switch can
    // re-anchor the page on the copy that becomes visible (TR2-P2-12, guard: local-view-anchored).
    <Box data-local-view-toggle={param} sx={{ display: "flex", minWidth: 0 }}>
    <SegmentTabs
      ariaLabel={ariaLabel}
      value={selected}
      tabs={options.map((option) => ({
        value: option.value,
        label: option.label,
        onClick: () => {
          if (option.value === selected) return;
          // Remember where the clicked toggle sits on screen. The pane swap can move the visible
          // copy (another card height, another slot), which read as the page jumping 725px on
          // /weighing/weights; after the swap, scroll so the visible copy is back under the pointer.
          const before = [...document.querySelectorAll<HTMLElement>(`[data-local-view-toggle="${param}"]`)].find((el) => el.offsetParent !== null);
          const beforeTop = before?.getBoundingClientRect().top;
          const url = new URL(window.location.href);
          if (option.value === defaultValue) url.searchParams.delete(param);
          else url.searchParams.set(param, option.value);
          replaceLocalOverlayUrl(`${url.pathname}${url.search}${url.hash}`);
          setSelected(option.value);
          window.dispatchEvent(
            new CustomEvent<ChangeDetail>(CHANGE_EVENT, { detail: { param, value: option.value } }),
          );
          if (beforeTop !== undefined) {
            requestAnimationFrame(() => {
              const after = [...document.querySelectorAll<HTMLElement>(`[data-local-view-toggle="${param}"]`)].find((el) => el.offsetParent !== null);
              if (after) window.scrollBy({ top: after.getBoundingClientRect().top - beforeTop, behavior: "instant" as ScrollBehavior });
            });
          }
        },
      }))}
    />
    </Box>
  );
}

/**
 * One view of a LocalViewToggle. Hidden views stay mounted, so switching back is instant and keeps
 * whatever the reader had open inside them. `display: contents` keeps the pane out of the page's
 * own grid, so wrapping a view does not change its layout.
 *
 * `stacked` (FIXJ12, guard: local-view-stacked): the panes share ONE grid cell (wrap them in
 * LocalViewStack). The hidden pane keeps its box (visibility hidden, inert), so the cell is always
 * as tall as the tallest view and a toggle pinned to each card's foot sits on the same pixel in every
 * view: switching never moves the toggle and never scrolls the page (/weighing/weights moved it
 * 398-725px and scrolled 257px at 390).
 */
export function LocalViewPane({
  param,
  value,
  current,
  stacked = false,
  children,
}: {
  param: string;
  /** The view this pane holds. */
  value: string;
  /** The view the server rendered as visible. */
  current: string;
  /** Overlay the panes in one LocalViewStack cell instead of display none. */
  stacked?: boolean;
  children: ReactNode;
}) {
  const [selected] = useLocalView(param, current);
  const visible = selected === value;
  if (stacked) {
    return (
      <Box
        aria-hidden={visible ? undefined : true}
        inert={!visible || undefined}
        sx={{ gridArea: "1 / 1", minWidth: 0, display: "flex", flexDirection: "column", visibility: visible ? "visible" : "hidden" }}
      >
        {children}
      </Box>
    );
  }
  return (
    <Box sx={{ display: visible ? "contents" : "none" }} aria-hidden={visible ? undefined : true}>
      {children}
    </Box>
  );
}

/** The one grid cell `stacked` LocalViewPanes overlay in (height 1: fills a stretched Grid item). */
export function LocalViewStack({ children }: { children: ReactNode }) {
  return <Box sx={{ display: "grid", height: 1, gridTemplateColumns: "minmax(0, 1fr)" }}>{children}</Box>;
}
