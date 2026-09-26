"use client";

import { Children, isValidElement, type ReactNode } from "react";
import Tab from "@mui/material/Tab";
import MuiTabs from "@mui/material/Tabs";
import Link from "@/components/no-prefetch-link";
import { shownTabValue } from "@/components/app/url-tab-nav";
import { useUrlTabNav } from "@/components/app/use-url-tab-nav";

// Template AccountLayout (sections/account/account-layout.tsx): MUI Tabs strip with one
// <Tab component={RouterLink}> per section. It lives in a client file because a server component
// cannot hand the Link function to MUI Tab as `component` ("Functions cannot be passed directly
// to Client Components"). The server strip still writes its tabs as console Links; this reads
// each one's href + label and renders it as a Tab that navigates through the same Link, in a
// transition (useUrlTabNav) so the page stays on screen and the pressed tab is selected at once.
export function RulebookTabStrip({ activeHref, children }: { activeHref: string; children: ReactNode }) {
  const tabs = Children.toArray(children).flatMap((child) => {
    if (!isValidElement<{ href?: unknown; children?: ReactNode }>(child)) return [];
    const href = child.props.href;
    return typeof href === "string" ? [{ href, label: child.props.children }] : [];
  });
  const { pendingValue, navigate } = useUrlTabNav();
  const shown = shownTabValue(activeHref, pendingValue);
  const known = tabs.some((tab) => tab.href === shown);
  return (
    <MuiTabs value={known ? shown : false} aria-busy={pendingValue !== null || undefined} variant="scrollable" allowScrollButtonsMobile>
      {tabs.map((tab) => (
        <Tab key={tab.href} component={Link} value={tab.href} href={tab.href} label={tab.label} onClick={(event: React.MouseEvent<HTMLElement>) => navigate(event, tab.href, tab.href)} />
      ))}
    </MuiTabs>
  );
}
