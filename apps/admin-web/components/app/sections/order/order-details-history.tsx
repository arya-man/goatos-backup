'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/order/order-details-history.tsx (@mui/lab Timeline, as the template). Anatomy
// guarded; the demo IOrderHistory becomes props: `title` (+ optional header `action`), `timeline`
// rows (title, a formatted `time` caption instead of fDateTime, an optional `body` line and `tone`
// dot colour), the dashed `summary` Paper rows (the template's four fixed timestamps), and an
// optional `more` link on its "Show more" button. The Card passes through sx / props (declared).
import type { ReactNode } from 'react';
import type { CardProps } from '@mui/material/Card';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Paper from '@mui/material/Paper';
import Timeline from '@mui/lab/Timeline';
import Button from '@mui/material/Button';
import TimelineDot from '@mui/lab/TimelineDot';
import CardHeader from '@mui/material/CardHeader';
import Typography from '@mui/material/Typography';
import TimelineContent from '@mui/lab/TimelineContent';
import TimelineSeparator from '@mui/lab/TimelineSeparator';
import TimelineConnector from '@mui/lab/TimelineConnector';
import TimelineItem, { timelineItemClasses } from '@mui/lab/TimelineItem';

import Link from '@/components/no-prefetch-link';
import { Iconify } from '@/components/minimal/iconify';

// ----------------------------------------------------------------------

export type OrderHistoryTone = 'primary' | 'grey' | 'success' | 'warning' | 'error' | 'info';

export type OrderHistoryItem = {
  key: string;
  title: ReactNode;
  time?: ReactNode;
  body?: ReactNode;
  tone?: OrderHistoryTone;
};

export type OrderHistorySummaryItem = { key: string; label: ReactNode; value: ReactNode };

type Props = Omit<CardProps, 'title'> & {
  title: ReactNode;
  action?: ReactNode;
  timeline: OrderHistoryItem[];
  summary?: OrderHistorySummaryItem[];
  more?: { href: string; label: string };
};

export function OrderDetailsHistory({ title, action, timeline, summary = [], more, sx, ...other }: Props) {
  const items = summary.map((item) => ({ key: item.key, label: item.label, value: item.value }));

  const renderSummary = () => (
    <Paper
      variant="outlined"
      sx={{
        p: 2.5,
        gap: 2,
        minWidth: 260,
        flexShrink: 0,
        borderRadius: 2,
        display: 'flex',
        typography: 'body2',
        borderStyle: 'dashed',
        flexDirection: 'column',
      }}
    >
      {items.map((item) => (
        <Box key={item.key} sx={{ gap: 0.5, display: 'flex', flexDirection: 'column' }}>
          <Box component="span" sx={{ color: 'text.secondary' }}>
            {item.label}
          </Box>
          {item.value}
        </Box>
      ))}
    </Paper>
  );

  const renderTimeline = () => (
    <Timeline
      sx={{
        p: 0,
        [`& .${timelineItemClasses.root}:before`]: { p: 0, flex: 0 },
      }}
    >
      {timeline.map((item, index) => {
        const firstTime = index === 0;
        const lastTime = index === timeline.length - 1;

        return (
          <TimelineItem key={item.key}>
            <TimelineSeparator>
              <TimelineDot color={item.tone ?? (firstTime ? 'primary' : 'grey')} />
              {lastTime ? null : <TimelineConnector />}
            </TimelineSeparator>

            <TimelineContent>
              <Typography variant="subtitle2">{item.title}</Typography>
              {item.body}
              <Box component="span" sx={{ color: 'text.disabled', typography: 'caption', mt: 0.5 }}>
                {item.time}
              </Box>
            </TimelineContent>
          </TimelineItem>
        );
      })}
    </Timeline>
  );

  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} action={action} />
      <Box
        sx={{
          p: 3,
          gap: 3,
          display: 'flex',
          alignItems: { md: 'flex-start' },
          flexDirection: { xs: 'column-reverse', md: 'row' },
        }}
      >
        <Box sx={{ flexGrow: 1 }}>
          {renderTimeline()}
          {more ? (
          <Button
            component={Link}
            href={more.href}
            size="small"
            color="inherit"
            endIcon={<Iconify icon="eva:arrow-ios-forward-fill" width={18} />}
          >
            {more.label}
          </Button>
          ) : null}
        </Box>
        {items.length ? renderSummary() : null}
      </Box>
    </Card>
  );
}
