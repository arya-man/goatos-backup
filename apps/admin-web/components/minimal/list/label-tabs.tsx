"use client";

// Copied from the licensed MUI Minimal template (sections/user/view/user-list-view.tsx status tabs):
// a Tabs strip where each tab carries a count Label.
import type { TabsProps } from '@mui/material/Tabs';
import type { LabelColor } from '../label';

import { varAlpha } from 'minimal-shared/utils';

import Tab from '@mui/material/Tab';
import Tabs from '@mui/material/Tabs';

import { Label } from '../label';
import { TAP_MIN } from '../_shared/tap';

export type LabelTabItem<V extends string = string> = {
  value: V;
  label: React.ReactNode;
  count?: number | string;
  color?: LabelColor;
};

export type LabelTabsProps<V extends string = string> = Omit<TabsProps, 'value' | 'onChange'> & {
  value: V;
  tabs: LabelTabItem<V>[];
  onChange: (value: V) => void;
};

export function LabelTabs<V extends string = string>({ value, tabs, onChange, sx, ...other }: LabelTabsProps<V>) {
  return (
    <Tabs
      value={value}
      onChange={(_event, next: V) => onChange(next)}
      variant="scrollable"
      scrollButtons={false}
      sx={[
        (theme) => ({
          px: { md: 2.5 },
          boxShadow: `inset 0 -2px 0 0 ${varAlpha(theme.vars.palette.grey['500Channel'], 0.08)}`,
          '& .MuiTab-root': { minHeight: TAP_MIN },
        }),
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      {tabs.map((tab) => (
        <Tab
          key={tab.value}
          iconPosition="end"
          value={tab.value}
          label={tab.label}
          icon={
            tab.count === undefined ? undefined : (
              <Label variant={tab.value === value ? 'filled' : 'soft'} color={tab.color ?? 'default'}>
                {tab.count}
              </Label>
            )
          }
        />
      ))}
    </Tabs>
  );
}
