"use client";

import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import Link from "@/components/no-prefetch-link";
import { Label } from "@/components/minimal/label";
import { shownTabValue } from "@/components/app/url-tab-nav";
import { useUrlTabNav } from "@/components/app/use-url-tab-nav";

export type SalesConfigTabItem = { value: string; label: string; href: string; count?: number };

// Template account view tabs (sections/account/account-layout.tsx: MUI Tabs, one Tab per section
// rendered as a router link) with the order list's count Label on a tab that has one
// (iconPosition "end"). Client file: a server component cannot hand the Link function to MUI Tab
// as `component`. Clicks navigate in a transition (useUrlTabNav), so the header stays on screen and
// the pressed tab is selected at once.
export function SalesConfigTabs({ ariaLabel, value, items }: { ariaLabel: string; value: string; items: SalesConfigTabItem[] }) {
  const { pendingValue, navigate } = useUrlTabNav();
  const shown = shownTabValue(value, pendingValue);
  return (
    <Tabs value={items.some((item) => item.value === shown) ? shown : false} aria-label={ariaLabel} aria-busy={pendingValue !== null || undefined} variant="scrollable" allowScrollButtonsMobile>
      {items.map((item) => (
        <Tab
          key={item.value}
          component={Link}
          value={item.value}
          href={item.href}
          label={item.label}
          iconPosition="end"
          icon={item.count === undefined ? undefined : <Label variant={item.value === shown ? "filled" : "soft"} color="default">{item.count}</Label>}
          onClick={(event: React.MouseEvent<HTMLElement>) => navigate(event, item.value, item.href)}
        />
      ))}
    </Tabs>
  );
}
