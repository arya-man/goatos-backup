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
  /** The Label count (a number, or the page's formatted figure); omitted = no Label. */
  count?: number | string;
  /** The Label colour (the template colours each status). */
  color?: LabelColor;
};

export function UrlTabs({
  items,
  value,
  ariaLabel,
  scrollButtons = false,
}: {
  items: UrlTabItem[];
  value: string;
  ariaLabel: string;
  /** "auto": more tabs than the card holds get MUI's scroll arrows (phones included, 44px taps), so
   *  a strip that overflows says so instead of cutting its last tab off (TR1-#29, /protocol-adherence
   *  with 12 work states). Default keeps the template's plain scrollable strip. */
  scrollButtons?: false | "auto";
}) {
  const { pendingValue, navigate } = useUrlTabNav();
  const active = shownTabValue(value, pendingValue);
  return (
    <Tabs
      value={items.some((item) => item.value === active) ? active : false}
      variant="scrollable"
      scrollButtons={scrollButtons}
      allowScrollButtonsMobile={scrollButtons === "auto"}
      aria-label={ariaLabel}
      aria-busy={pendingValue !== null || undefined}
      sx={[
        (theme) => ({
          px: scrollButtons === "auto" ? 0.5 : 2.5,
          ...(scrollButtons === "auto" ? { "& .MuiTabs-scrollButtons": { width: "var(--tap-min)", flexShrink: 0 }, "& .MuiTabs-scrollButtons.Mui-disabled": { opacity: 0.3 } } : {}),
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
