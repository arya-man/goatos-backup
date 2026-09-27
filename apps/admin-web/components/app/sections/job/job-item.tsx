'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/job/job-item.tsx. Anatomy guarded; the demo IJobItem becomes props / SLOTS: `avatar`
// (the rounded 48px company logo: an icon or initial), `title` (its detail Link), `secondary` (its
// "Posted date" line), `meta` (its candidates caption line), `media` (a declared slot under it),
// `facts` (its four icon + caption grid cells), `menuActions` (its View / Edit / Delete ⋮ menu;
// the ⋮ renders only with items; `menuLabel` is its declared accessible name, required with items)
// and `children` (a declared dashed-divider section at the foot).
import type { ReactNode } from 'react';
import type { CardProps } from '@mui/material/Card';
import type { SxProps, Theme } from '@mui/material/styles';

import { usePopover } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Avatar from '@mui/material/Avatar';
import Divider from '@mui/material/Divider';
import MenuList from '@mui/material/MenuList';
import MenuItem from '@mui/material/MenuItem';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import ListItemText from '@mui/material/ListItemText';

import { mergeSx } from '@/components/app/merge-sx';
import { Iconify } from '@/components/minimal/iconify';
import { CustomPopover } from '@/components/minimal/custom-popover';

// ----------------------------------------------------------------------

export type JobItemFact = { key: string; icon?: ReactNode; label: ReactNode };
export type JobItemMenuAction = { key: string; label: ReactNode; icon?: ReactNode; onClick: () => void };

/** The ⋮ renders only with items, and then it must carry an accessible name (axe button-name). */
type MenuProps =
  | { menuActions?: undefined; menuLabel?: undefined }
  | { menuActions: JobItemMenuAction[]; menuLabel: string };

type Props = Omit<CardProps, 'title'> & MenuProps & {
  title: ReactNode;
  secondary?: ReactNode;
  meta?: ReactNode;
  avatar?: ReactNode;
  avatarUrl?: string;
  media?: ReactNode;
  facts?: JobItemFact[];
  children?: ReactNode;
  /** Declared overrides (template-derived.json): the meta line wraps; fact captions wrap. */
  slotProps?: { meta?: SxProps<Theme> };
  wrapFacts?: boolean;
};

export function JobItem({ title, secondary, meta, avatar, avatarUrl, media, facts = [], menuActions = [], menuLabel, children, slotProps, wrapFacts, sx, ...other }: Props) {
  const menuActionsPopover = usePopover();

  const renderMenuActions = () => (
    <CustomPopover
      open={menuActionsPopover.open}
      anchorEl={menuActionsPopover.anchorEl}
      onClose={menuActionsPopover.onClose}
      slotProps={{ arrow: { placement: 'right-top' } }}
    >
      <MenuList>
        {menuActions.map((action) => (
          <MenuItem
            key={action.key}
            onClick={() => {
              menuActionsPopover.onClose();
              action.onClick();
            }}
          >
            {action.icon}
            {action.label}
          </MenuItem>
        ))}
      </MenuList>
    </CustomPopover>
  );

  return (
    <>
      <Card sx={sx} {...other}>
        {menuActions.length ? (
        <IconButton onClick={menuActionsPopover.onOpen} aria-label={menuLabel} sx={{ position: 'absolute', top: 8, right: 8 }}>
          <Iconify icon="eva:more-vertical-fill" />
        </IconButton>
        ) : null}

        <Box sx={{ p: 3, pb: 2 }}>
          <Avatar
            alt={typeof title === 'string' ? title : undefined}
            src={avatarUrl}
            variant="rounded"
            sx={{ width: 48, height: 48, mb: 2 }}
          >
            {avatar}
          </Avatar>

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

          <Box
            sx={mergeSx({
              gap: 0.5,
              display: 'flex',
              alignItems: 'center',
              color: 'primary.main',
              typography: 'caption',
            }, slotProps?.meta)}
          >
            {meta}
          </Box>

          {media}
        </Box>

        {facts.length ? (
        <>
        <Divider sx={{ borderStyle: 'dashed' }} />

        <Box
          sx={{
            p: 3,
            rowGap: 1.5,
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
                flexShrink: 0,
                display: 'flex',
                alignItems: 'center',
                color: 'text.disabled',
              }}
            >
              {item.icon}
              <Typography variant="caption" noWrap={!wrapFacts}>
                {item.label}
              </Typography>
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

      {renderMenuActions()}
    </>
  );
}
