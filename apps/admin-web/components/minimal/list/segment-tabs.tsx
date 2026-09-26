'use client';

// The template's segmented tab strip: MUI Tabs with indicatorColor="custom" (theme/core/components/
// tabs.tsx — the white/grey pill indicator), one scrollable row. Presentation only: each tab is a
// real link (href) or a button, and the caller owns what a click does.
import type { Theme, SxProps } from '@mui/material/styles';

import Tab from '@mui/material/Tab';
import Tabs from '@mui/material/Tabs';

import Link from '@/components/no-prefetch-link';

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
  const known = tabs.some((tab) => tab.value === value);
  return (
    <Tabs
      value={known ? value : false}
      indicatorColor="custom"
      variant="scrollable"
      scrollButtons={false}
      aria-label={ariaLabel}
      aria-busy={busy || undefined}
      className={className}
      sx={[{ width: 'fit-content', maxWidth: '100%' }, ...(Array.isArray(sx) ? sx : [sx])]}
    >
      {tabs.map((tab) =>
        tab.href ? (
          <Tab key={tab.value} value={tab.value} label={tab.label} component={Link} href={tab.href} scroll={keepScroll ? false : undefined} onClick={tab.onClick} disabled={tab.disabled} data-testid={tab.testId} />
        ) : (
          <Tab key={tab.value} value={tab.value} label={tab.label} onClick={tab.onClick} disabled={tab.disabled} data-testid={tab.testId} />
        ),
      )}
    </Tabs>
  );
}
