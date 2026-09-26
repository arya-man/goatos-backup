"use client";

// The template list's status Tabs (sections/order/view/order-list-view.tsx: MUI Tabs + Tab with the
// count as a Label at iconPosition end, px 2.5, the grey inset bottom rule) for tabs whose state is a
// search param. Every tab is a real link; a plain click navigates through useUrlTabNav (announced, so
// the page's UrlSuspense panels swap to their skeleton at once; guard: url-keyed-panel) and the clicked
// tab is drawn selected immediately. Renders only MUI + template parts.
import type { MouseEvent } from "react";
import type { LabelColor } from "@/components/minimal/label";

import { varAlpha } from "minimal-shared/utils";

import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";

import Link from "@/components/no-prefetch-link";
import { Label } from "@/components/minimal/label";
import { shownTabValue } from "@/components/app/url-tab-nav";
import { useUrlTabNav } from "@/components/app/use-url-tab-nav";

export type UrlTabItem = {
  value: string;
  label: string;
  href: string;
  /** The Label count; omitted = no Label. */
  count?: number;
  /** The Label colour (the template colours each status). */
  color?: LabelColor;
};

export function UrlTabs({ items, value, ariaLabel }: { items: UrlTabItem[]; value: string; ariaLabel: string }) {
  const { pendingValue, navigate } = useUrlTabNav();
  const active = shownTabValue(value, pendingValue);
  return (
    <Tabs
      value={items.some((item) => item.value === active) ? active : false}
      variant="scrollable"
      scrollButtons={false}
      aria-label={ariaLabel}
      aria-busy={pendingValue !== null || undefined}
      sx={[
        (theme) => ({
          px: 2.5,
          boxShadow: `inset 0 -2px 0 0 ${varAlpha(theme.vars.palette.grey["500Channel"], 0.08)}`,
        }),
      ]}
    >
      {items.map((item) => (
        <Tab
          key={item.value}
          value={item.value}
          label={item.label}
          iconPosition="end"
          icon={
            item.count != null ? (
              <Label variant={item.value === active ? "filled" : "soft"} color={item.color ?? "default"}>
                {item.count}
              </Label>
            ) : undefined
          }
          component={Link}
          href={item.href}
          scroll={false}
          onClick={(event: MouseEvent<HTMLElement>) => navigate(event, item.value, item.href)}
        />
      ))}
    </Tabs>
  );
}
