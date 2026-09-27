"use client";

import Link from "@/components/no-prefetch-link";
import { type MouseEvent, type ReactNode } from "react";
import { varAlpha } from "minimal-shared/utils";
import Box from "@mui/material/Box";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import type { SxProps, Theme } from "@mui/material/styles";
import { Label } from "@/components/minimal/label";
import { shownTabValue } from "@/components/app/url-tab-nav";
import { useUrlTabNav } from "@/components/app/use-url-tab-nav";
import Stack from "@mui/material/Stack";
import { cx } from "@/lib/tone";

export type TemplateTabItem = {
  value: string;
  label: ReactNode;
  icon?: ReactNode;
  /** Count Label after the label (template list tabs). */
  count?: ReactNode;
  /** Render as a link (URL-driven tabs). */
  href?: string;
  disabled?: boolean;
};

export type TemplateTabsProps = {
  items: TemplateTabItem[];
  value: string;
  onChange?: (value: string) => void;
  /** "underline" = template Tabs; "pill" = template Tabs `indicatorColor="custom"` (segmented). */
  variant?: "underline" | "pill";
  ariaLabel?: string;
  className?: string;
  /** "brand": the active tab's count Label is the primary colour. */
  countTone?: "brand";
  /** Template scroll arrows (`scrollButtons="auto"`, also on touch) for strips that overflow a phone. */
  scrollButtons?: "auto";
  sx?: SxProps<Theme>;
};

/**
 * The template list tab strip (sections/user/view/user-list-view.tsx): MUI `Tabs` + `Tab`, counts as
 * template `Label`s at iconPosition end, the grey inset bottom rule. One scrollable row at every
 * width. Link-driven strips navigate through `useUrlTabNav`: the clicked tab is selected NOW and the
 * page's UrlSuspense panels swap to their skeleton (guard: url-keyed-panel). Renders only MUI +
 * template parts.
 *
 * A plain function, never `memo()` (TR1-#38, guard: tab-strip-no-memo): a memo'd client component
 * rendered from a server page remounted on every same-route navigation (the RSC reconcile did not
 * match the memo wrapper), so each tab / filter click tore the strip down and rebuilt it.
 */
export function TemplateTabs({ items, value, onChange, variant = "underline", ariaLabel, className, countTone, scrollButtons, sx }: TemplateTabsProps) {
  const { pendingValue, navigate } = useUrlTabNav();
  const active = shownTabValue(value, pendingValue);
  const pending = pendingValue !== null;
  const known = items.some((item) => item.value === active);

  return (
    <Tabs
      value={known ? active : false}
      onChange={(_event, next: string) => {
        if (!items.find((item) => item.value === next)?.href) onChange?.(next);
      }}
      variant="scrollable"
      scrollButtons={scrollButtons ?? false}
      allowScrollButtonsMobile={scrollButtons === "auto"}
      indicatorColor={variant === "pill" ? "custom" : undefined}
      aria-label={ariaLabel}
      aria-busy={pending || undefined}
      className={cx("kit-tabs", className)}
      sx={[
        variant === "pill"
          ? { width: "fit-content", maxWidth: "100%" }
          : (theme) => ({ boxShadow: `inset 0 -2px 0 0 ${varAlpha(theme.vars.palette.grey["500Channel"], 0.08)}` }),
        ...(Array.isArray(sx) ? sx : sx ? [sx] : []),
      ]}
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
              onClick={(event: MouseEvent<HTMLElement>) => {
                onChange?.(item.value);
                navigate(event, item.value, href);
              }}
            />
          );
        }
        return <Tab key={item.value} value={item.value} label={label} icon={count} iconPosition="end" disabled={item.disabled} />;
      })}
    </Tabs>
  );
}

/**
 * The active tab's body: an MUI Stack (spacing 3, the template's section rhythm) keyed by the tab,
 * so a switch mounts the new tab's blocks. No hand-made crossfade (the template has none).
 */
export function TabPanel({ tabKey, children, className }: { tabKey: string; children: ReactNode; stagger?: number; className?: string }) {
  return (
    <Stack key={tabKey} spacing={3} className={className} sx={{ minWidth: 0 }}>
      {children}
    </Stack>
  );
}
