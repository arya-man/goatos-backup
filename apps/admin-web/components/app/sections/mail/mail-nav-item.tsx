'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/mail/mail-nav-item.tsx. Anatomy guarded; the demo IMailLabel becomes `label`
// ({ name, count, icon?, color? }); the item is a link (`href`, Next Link, scroll kept, aria-current)
// because our rails switch a URL param; declared slot `action` (an edit IconButton beside the
// button) and overrides `slotProps.item` (nested-list indent / alignment on the li) and
// `slotProps.button` (the 44px phone tap floor).
import type { ReactNode } from 'react';
import type { SxProps, Theme } from '@mui/material/styles';
import type { ListItemButtonProps } from '@mui/material/ListItemButton';
import type { IconifyName } from '@/components/minimal/iconify';

import Box from '@mui/material/Box';
import ListItemButton from '@mui/material/ListItemButton';

import Link from '@/components/no-prefetch-link';
import { mergeSx } from '@/components/app/merge-sx';
import { Iconify } from '@/components/minimal/iconify';

// ----------------------------------------------------------------------

export type MailNavLabel = {
  name: string;
  count?: number | string;
  icon?: IconifyName;
  color?: string;
};

type Props = Omit<ListItemButtonProps, 'action'> & {
  selected: boolean;
  label: MailNavLabel;
  href: string;
  onClickNavItem?: () => void;
  action?: ReactNode;
  slotProps?: { item?: SxProps<Theme>; button?: SxProps<Theme>; label?: SxProps<Theme> };
};

export function MailNavItem({ selected, label, href, onClickNavItem, action, slotProps, ...other }: Props) {
  return (
    <Box component="li" sx={mergeSx({ display: 'flex' }, slotProps?.item)}>
      <ListItemButton
        disableGutters
        component={Link}
        href={href}
        scroll={false}
        aria-current={selected ? 'page' : undefined}
        onClick={onClickNavItem}
        sx={mergeSx({
          pl: 1,
          pr: 1.5,
          gap: 2,
          borderRadius: 0.75,
          color: 'text.secondary',
          ...(selected && { color: 'text.primary' }),
        }, slotProps?.button)}
        {...other}
      >
        {label.icon ? <Iconify icon={label.icon} width={22} sx={{ color: label.color }} /> : null}

        <Box
          component="span"
          sx={mergeSx({
            flexGrow: 1,
            textTransform: 'capitalize',
            typography: selected ? 'subtitle2' : 'body2',
          }, slotProps?.label)}
        >
          {label.name}
        </Box>

        {label.count !== undefined && (
          <Box component="span" sx={{ typography: 'caption' }}>
            {label.count}
          </Box>
        )}
      </ListItemButton>

      {action}
    </Box>
  );
}
