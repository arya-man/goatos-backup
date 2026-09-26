"use client";

import Link from "@/components/no-prefetch-link";
import { Children, Component, memo, useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { varAlpha } from "minimal-shared/utils";
import Box from "@mui/material/Box";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import { Label } from "@/components/minimal/label";
import { cx } from "@/lib/tone";

export type AnimatedTabItem = {
  value: string;
  label: ReactNode;
  icon?: ReactNode;
  /** Count Label after the label (template list tabs). */
  count?: ReactNode;
  /** Render as a link (URL-driven tabs). */
  href?: string;
  disabled?: boolean;
};

export type AnimatedTabsProps = {
  items: AnimatedTabItem[];
  value: string;
  onChange?: (value: string) => void;
  /** "underline" = template Tabs; "pill" = template Tabs `indicatorColor="custom"` (segmented). */
  variant?: "underline" | "pill";
  ariaLabel?: string;
  className?: string;
  /** "brand": the active tab's count Label is the primary colour. */
  countTone?: "brand";
};

/**
 * The tab strip: the template's MUI `Tabs` + `Tab`, counts as `Label`s (as in the template user
 * list). One scrollable row at every width. Link-driven strips navigate on the server, so the
 * clicked tab becomes active NOW and the strip shows a pending line until the URL catches up; a
 * stale guess is dropped the moment `value` moves.
 */
export const AnimatedTabs = memo(function AnimatedTabs({ items, value, onChange, variant = "underline", ariaLabel, className, countTone }: AnimatedTabsProps) {
  const [optimistic, setOptimistic] = useState<string | null>(null);
  const active = optimistic && optimistic !== value ? optimistic : value;
  const pending = optimistic !== null && optimistic !== value;
  const known = items.some((item) => item.value === active);

  return (
    <Tabs
      value={known ? active : false}
      onChange={(_event, next: string) => {
        if (!items.find((item) => item.value === next)?.href) onChange?.(next);
      }}
      variant="scrollable"
      scrollButtons={false}
      indicatorColor={variant === "pill" ? "custom" : undefined}
      aria-label={ariaLabel}
      aria-busy={pending || undefined}
      className={cx("kit-tabs", pending && "kit-tabs-pending", className)}
      sx={
        variant === "pill"
          ? { width: "fit-content", maxWidth: "100%" }
          : (theme) => ({ boxShadow: `inset 0 -2px 0 0 ${varAlpha(theme.vars.palette.grey["500Channel"], 0.08)}` })
      }
    >
      {items.map((item) => {
        const isActive = item.value === active;
        const label = item.icon ? (
          <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
            {item.icon}
            {item.label}
          </Box>
        ) : (
          item.label
        );
        const count =
          item.count != null ? (
            <Label variant={isActive ? "filled" : "soft"} color={isActive && countTone === "brand" ? "primary" : "default"}>
              {item.count}
            </Label>
          ) : undefined;
        if (item.href && !item.disabled) {
          const href = item.href;
          return (
            <Tab
              key={item.value}
              value={item.value}
              label={label}
              icon={count}
              iconPosition="end"
              component={Link}
              href={href}
              scroll={false}
              onClick={() => {
                setOptimistic(item.value);
                onChange?.(item.value);
                // Same event the segmented links fire, so a route's pending-tab skeleton
                // (WeightsAnalyticsTabLoading) works under the strip without a callback prop.
                window.dispatchEvent(new CustomEvent("metricseg:navigate", { detail: { value: item.value, href } }));
              }}
            />
          );
        }
        return <Tab key={item.value} value={item.value} label={label} icon={count} iconPosition="end" disabled={item.disabled} />;
      })}
    </Tabs>
  );
});

function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;
}

const CROSSFADE_MS = 180;

type PanelSnapshot = { el: HTMLElement; height: number };

/**
 * The live panel. A class, because `getSnapshotBeforeUpdate` is the one React hook that runs
 * AFTER render and BEFORE the DOM is mutated — the only moment the outgoing panel can be cloned
 * exactly as it was on screen. It clones only on a key change, never on ordinary re-renders.
 */
class LivePanel extends Component<{ tabKey: string; children: ReactNode; onSwitch: (snapshot: PanelSnapshot, previousKey: string) => void }> {
  node: HTMLDivElement | null = null;

  getSnapshotBeforeUpdate(prevProps: { tabKey: string }): PanelSnapshot | null {
    if (prevProps.tabKey === this.props.tabKey || !this.node) return null;
    return { el: this.node.cloneNode(true) as HTMLElement, height: this.node.offsetHeight };
  }

  componentDidUpdate(prevProps: { tabKey: string }, _prevState: unknown, snapshot: PanelSnapshot | null): void {
    if (snapshot) this.props.onSwitch(snapshot, prevProps.tabKey);
  }

  render(): ReactNode {
    return (
      <div ref={(node) => { this.node = node; }} className="kit-tabpanel-inner kit-tabpanel-in">
        {this.props.children}
      </div>
    );
  }
}

/**
 * Content transition for tab switches.
 *
 * Wrap the body that belongs to the active tab and give the wrapper the active tab's value as
 * `tabKey`. On a switch the OUTGOING panel is kept for one crossfade as a DOM clone (not a
 * re-render of its React tree, which would re-run mount effects — a chart drawing in again while
 * fading out), absolutely positioned over the incoming one, and the wrapper reserves
 * `max(outgoing, incoming)` height for the 180ms of the transition — so the page never jumps and
 * no frame shows an empty or double-height box. The incoming panel's direct children rise in with
 * a small stagger. Reduced-motion: an instant swap.
 */
export function TabPanel({ tabKey, children, stagger = 0.045, className }: { tabKey: string; children: ReactNode; stagger?: number; className?: string }) {
  const [outgoing, setOutgoing] = useState<{ key: string; el: HTMLElement; height: number } | null>(null);
  const [reserve, setReserve] = useState<number | null>(null);
  const wrapper = useRef<HTMLDivElement | null>(null);
  const ghost = useRef<HTMLDivElement | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const onSwitch = useCallback((snapshot: PanelSnapshot, previousKey: string) => {
    if (prefersReducedMotion()) return;
    setOutgoing({ key: previousKey, el: snapshot.el, height: snapshot.height });
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      setOutgoing(null);
      setReserve(null);
      timer.current = null;
    }, CROSSFADE_MS);
  }, []);

  // Mount the clone into the ghost slot and reserve max(outgoing, incoming) for the crossfade.
  useLayoutEffect(() => {
    const slot = ghost.current;
    if (!slot || !outgoing) return;
    slot.replaceChildren(outgoing.el);
    const incoming = wrapper.current?.querySelector<HTMLElement>(".kit-tabpanel-in")?.offsetHeight ?? 0;
    setReserve(Math.max(outgoing.height, incoming));
    return () => { slot.replaceChildren(); };
  }, [outgoing]);

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  const items = Children.toArray(children);
  return (
    <div ref={wrapper} className={cx("kit-tabpanel", outgoing && "is-switching", className)} style={reserve != null ? { minHeight: reserve } : undefined}>
      <LivePanel tabKey={tabKey} onSwitch={onSwitch}>
        {items.map((child, i) => (
          <div
            key={`${tabKey}:${i}`}
            className="kit-tabpanel-item"
            style={{ "--kit-tab-delay": `${Math.round(i * stagger * 1000)}ms` } as CSSProperties}
          >
            {child}
          </div>
        ))}
      </LivePanel>
      {outgoing ? <div ref={ghost} className="kit-tabpanel-out" aria-hidden="true" /> : null}
    </div>
  );
}
