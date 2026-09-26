// Copied from the licensed MUI Minimal template (next-ts src/sections/job/job-item.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - the job mock is replaced by the page's parts: the rounded 48px avatar (an icon or initial),
//    the subtitle1 title, the caption secondary line, the primary-coloured caption meta line (chips
//    allowed), and the 2-column facts grid under the dashed divider, whose cells may carry a
//    label/value pair instead of an icon + caption;
//  - the View/Edit/Delete CustomPopover is the page's optional `action` node (top-right slot);
//  - `media` renders inside the top block (e.g. a capture strip) and `children` renders as one
//    more dashed-divider section under the facts (answers, a decision form) — the same Card rhythm.

import type { ReactNode } from 'react';
import type { CardProps } from '@mui/material/Card';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Avatar from '@mui/material/Avatar';
import Divider from '@mui/material/Divider';
import Typography from '@mui/material/Typography';
import ListItemText from '@mui/material/ListItemText';

// ----------------------------------------------------------------------

export type JobItemFact = {
  key: string;
  /** The template's icon + caption cell. */
  icon?: ReactNode;
  label: ReactNode;
  /** With a value the cell reads label (caption, disabled) over value (body2). */
  value?: ReactNode;
};

type Props = Omit<CardProps, 'title'> & {
  title: ReactNode;
  secondary?: ReactNode;
  meta?: ReactNode;
  avatar?: ReactNode;
  action?: ReactNode;
  media?: ReactNode;
  facts?: JobItemFact[];
  children?: ReactNode;
};

export function JobItem({ title, secondary, meta, avatar, action, media, facts = [], children, sx, ...other }: Props) {
  return (
    <Card sx={[{ position: 'relative' }, ...(Array.isArray(sx) ? sx : [sx])]} {...other}>
      {action ? <Box sx={{ position: 'absolute', top: 8, right: 8 }}>{action}</Box> : null}

      <Box sx={{ p: 3, pb: 2 }}>
        {avatar ? (
          <Avatar variant="rounded" sx={{ width: 48, height: 48, mb: 2, bgcolor: 'background.neutral', color: 'text.secondary' }}>
            {avatar}
          </Avatar>
        ) : null}

        <ListItemText
          sx={{ mb: 1 }}
          primary={title}
          secondary={secondary}
          slotProps={{
            primary: { sx: { typography: 'subtitle1' } },
            secondary: {
              sx: { mt: 1, typography: 'caption', color: 'text.disabled' },
            },
          }}
        />

        {meta ? (
          <Box
            sx={{
              gap: 0.5,
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              color: 'primary.main',
              typography: 'caption',
            }}
          >
            {meta}
          </Box>
        ) : null}

        {media ? <Box sx={{ mt: 2 }}>{media}</Box> : null}
      </Box>

      {facts.length ? (
        <>
          <Divider sx={{ borderStyle: 'dashed' }} />

          <Box
            sx={{
              p: 3,
              rowGap: 1.5,
              columnGap: 2,
              display: 'grid',
              gridTemplateColumns: 'repeat(2, 1fr)',
            }}
          >
            {facts.map((item) => (
              <Box
                key={item.key}
                sx={{
                  gap: 0.5,
                  minWidth: 0,
                  display: 'flex',
                  alignItems: item.value === undefined ? 'center' : 'flex-start',
                  flexDirection: item.value === undefined ? 'row' : 'column',
                  color: 'text.disabled',
                }}
              >
                {item.icon}
                <Typography variant="caption" noWrap={item.value === undefined}>
                  {item.label}
                </Typography>
                {item.value === undefined ? null : (
                  <Typography variant="body2" sx={{ color: 'text.primary', overflowWrap: 'anywhere' }}>
                    {item.value}
                  </Typography>
                )}
              </Box>
            ))}
          </Box>
        </>
      ) : null}

      {children ? (
        <>
          <Divider sx={{ borderStyle: 'dashed' }} />
          <Box sx={{ p: 3 }}>{children}</Box>
        </>
      ) : null}
    </Card>
  );
}
