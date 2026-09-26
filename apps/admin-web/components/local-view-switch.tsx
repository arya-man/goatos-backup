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
import { replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { SegmentTabs } from "@/components/minimal/list/segment-tabs";

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
    <SegmentTabs
      className="metricseg"
      ariaLabel={ariaLabel}
      value={selected}
      tabs={options.map((option) => ({
        value: option.value,
        label: option.label,
        onClick: () => {
          if (option.value === selected) return;
          const url = new URL(window.location.href);
          if (option.value === defaultValue) url.searchParams.delete(param);
          else url.searchParams.set(param, option.value);
          replaceLocalOverlayUrl(`${url.pathname}${url.search}${url.hash}`);
          setSelected(option.value);
          window.dispatchEvent(
            new CustomEvent<ChangeDetail>(CHANGE_EVENT, { detail: { param, value: option.value } }),
          );
        },
      }))}
    />
  );
}

/**
 * One view of a LocalViewToggle. Hidden views stay mounted, so switching back is instant and keeps
 * whatever the reader had open inside them. `display: contents` keeps the pane out of the page's
 * own grid, so wrapping a view does not change its layout.
 */
export function LocalViewPane({
  param,
  value,
  current,
  children,
}: {
  param: string;
  /** The view this pane holds. */
  value: string;
  /** The view the server rendered as visible. */
  current: string;
  children: ReactNode;
}) {
  const [selected] = useLocalView(param, current);
  const visible = selected === value;
  return (
    <div style={{ display: visible ? "contents" : "none" }} aria-hidden={visible ? undefined : true}>
      {children}
    </div>
  );
}
