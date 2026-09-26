'use client';

// Copied from the licensed MUI Minimal template (next-ts src/sections/order/order-details-history.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - the card title, timeline rows and the dashed summary Paper take the page's rows (backend copy
//    and already-formatted dates) instead of the order mock;
//  - @mui/lab is not a dependency of admin-web, so the lab Timeline parts are drawn with the same
//    geometry the lab ships (12px TimelineDot with a 2px ring, 2px TimelineConnector, 6px 16px
//    content padding, `:before` flex 0) in plain MUI Box — the template's look, no new package;
//  - a row may carry a status tone (the dot colour) and a body line; "Show more" is an optional link.

import type { ReactNode } from 'react';
import type { CardProps } from '@mui/material/Card';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Paper from '@mui/material/Paper';
import Button from '@mui/material/Button';
import CardHeader from '@mui/material/CardHeader';
import Typography from '@mui/material/Typography';

import Link from '@/components/no-prefetch-link';

import { Iconify } from '@/components/minimal/iconify';

// ----------------------------------------------------------------------

export type OrderHistoryTone = 'primary' | 'grey' | 'success' | 'warning' | 'error' | 'info';

export type OrderHistoryItem = {
  key: string;
  title: ReactNode;
  /** Caption under the title (the template's fDateTime line). */
  time?: ReactNode;
  /** Optional detail line (e.g. who / what changed). */
  body?: ReactNode;
  /** Dot colour; the template fills the first row primary and the rest grey. */
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

export function OrderDetailsHistory({ title, action, timeline, summary, more, sx, ...other }: Props) {
  const renderSummary = () => (
    <Paper
      variant="outlined"
      sx={{
        p: 2.5,
        gap: 2,
        minWidth: { md: 260 },
        flexShrink: 0,
        borderRadius: 2,
        display: 'flex',
        typography: 'body2',
        borderStyle: 'dashed',
        flexDirection: 'column',
      }}
    >
      {summary?.map((item) => (
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
    <Box component="ul" sx={{ p: 0, m: 0, listStyle: 'none', display: 'flex', flexDirection: 'column' }}>
      {timeline.map((item, index) => {
        const firstTime = index === 0;
        const lastTime = index === timeline.length - 1;
        const tone = item.tone ?? (firstTime ? 'primary' : 'grey');

        return (
          <Box component="li" key={item.key} sx={{ display: 'flex', minHeight: 70 }}>
            {/* TimelineSeparator */}
            <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', flex: 0 }}>
              <Box
                component="span"
                sx={(theme) => ({
                  my: 1.5,
                  p: '4px',
                  borderRadius: '50%',
                  borderWidth: 2,
                  borderStyle: 'solid',
                  borderColor: 'transparent',
                  boxShadow: theme.vars.shadows[1],
                  bgcolor: tone === 'grey' ? theme.vars.palette.grey[400] : theme.vars.palette[tone].main,
                })}
              />
              {lastTime ? null : <Box component="span" sx={{ width: 2, flexGrow: 1, bgcolor: 'divider' }} />}
            </Box>

            {/* TimelineContent */}
            <Box sx={{ py: '6px', px: 2, flex: 1, minWidth: 0 }}>
              <Typography variant="subtitle2">{item.title}</Typography>
              {item.body ? (
                <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>
                  {item.body}
                </Typography>
              ) : null}
              {item.time ? (
                <Box component="span" sx={{ color: 'text.disabled', typography: 'caption', mt: 0.5, display: 'block' }}>
                  {item.time}
                </Box>
              ) : null}
            </Box>
          </Box>
        );
      })}
    </Box>
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
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
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
        {summary && summary.length ? renderSummary() : null}
      </Box>
    </Card>
  );
}
