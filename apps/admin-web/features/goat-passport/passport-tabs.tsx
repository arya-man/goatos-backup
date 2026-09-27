"use client";

// Template account tabs (sections/account/account-layout.tsx): page-level icon Tabs under the
// breadcrumbs, one tab per passport section, each a URL tab (guard: url-keyed-panel). A client leaf
// so the Link `component` never crosses the server/client boundary (FJ1 P0-1, guard:
// server-function-prop).
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import Link from "@/components/no-prefetch-link";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { shownTabValue } from "@/components/app/url-tab-nav";
import { useUrlTabNav } from "@/components/app/use-url-tab-nav";

export type PassportTab = { value: string; href: string; label: string; icon: IconifyName };

export function PassportTabs({ selectedTab, tabs, ariaLabel }: { selectedTab: string; tabs: PassportTab[]; ariaLabel: string }) {
  const { pendingValue, navigate } = useUrlTabNav();
  return (
    <Tabs
      value={shownTabValue(selectedTab, pendingValue)}
      aria-label={ariaLabel}
      aria-busy={pendingValue !== null || undefined}
      variant="scrollable"
      allowScrollButtonsMobile
      sx={{ mb: { xs: 3, md: 5 } }}
    >
      {tabs.map((tab) => (
        <Tab
          key={tab.value || "summary"}
          component={Link}
          value={tab.value}
          href={tab.href}
          label={tab.label}
          icon={<Iconify width={24} icon={tab.icon} />}
          onClick={(event: React.MouseEvent<HTMLElement>) => navigate(event, tab.value, tab.href)}
        />
      ))}
    </Tabs>
  );
}
