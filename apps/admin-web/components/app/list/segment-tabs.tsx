'use client';

// The template's segmented tab strip: MUI Tabs with indicatorColor="custom" (theme/core/components/
// tabs.tsx — the white/grey pill indicator), one scrollable row. Presentation only: each tab is a
// real link (href) or a button, and the caller owns what a click does. A link tab the caller does not
// handle (no preventDefault) navigates in a transition (useUrlTabNav): the page stays on screen and
// the pressed tab is drawn selected at once.
// A standalone strip is rounded like the template BankingOverview Tabs (16px, var(--r-xl): the 8px
// pill plus its 8px inset), never a square grey band (TR1-#32, guard: segment-tabs-rounded).
import type { Theme, SxProps } from '@mui/material/styles';

import Tab from '@mui/material/Tab';
import Tabs from '@mui/material/Tabs';

import Link from '@/components/no-prefetch-link';
import { shownTabValue } from '@/components/app/url-tab-nav';
import { useUrlTabNav } from '@/components/app/use-url-tab-nav';

export type SegmentTab = {
  value: string;
  label: React.ReactNode;
  /** Renders the tab as a link to this href (modified clicks keep browser behaviour). */
  href?: string;
  onClick?: (event: React.MouseEvent<HTMLElement>) => void;
  disabled?: boolean;
  testId?: string;
};

export type SegmentTabsProps = {
  value: string;
  tabs: readonly SegmentTab[];
  ariaLabel?: string;
  /** Pressed-but-not-yet-navigated state (aria-busy + the caller's pending class). */
  busy?: boolean;
  /** Link tabs keep the scroll position (Next Link `scroll={false}`). */
  keepScroll?: boolean;
  className?: string;
  sx?: SxProps<Theme>;
};

export function SegmentTabs({ value, tabs, ariaLabel, busy, keepScroll = false, className, sx }: SegmentTabsProps) {
  const { pendingValue, navigate } = useUrlTabNav();
  const shown = shownTabValue(value, pendingValue);
  const known = tabs.some((tab) => tab.value === shown);
  return (
    <Tabs
      value={known ? shown : false}
      indicatorColor="custom"
      variant="scrollable"
      scrollButtons={false}
      aria-label={ariaLabel}
      aria-busy={busy || pendingValue !== null || undefined}
      className={className}
      sx={[{ width: 'fit-content', maxWidth: '100%', borderRadius: 'var(--r-xl)' }, ...(Array.isArray(sx) ? sx : [sx])]}
    >
      {tabs.map((tab) =>
        tab.href ? (
          <Tab key={tab.value} value={tab.value} label={tab.label} component={Link} href={tab.href} scroll={keepScroll ? false : undefined} onClick={(event: React.MouseEvent<HTMLElement>) => {
            tab.onClick?.(event);
            navigate(event, tab.value, tab.href as string);
          }} disabled={tab.disabled} data-testid={tab.testId} />
        ) : (
          <Tab key={tab.value} value={tab.value} label={tab.label} onClick={tab.onClick} disabled={tab.disabled} data-testid={tab.testId} />
        ),
      )}
    </Tabs>
  );
}
