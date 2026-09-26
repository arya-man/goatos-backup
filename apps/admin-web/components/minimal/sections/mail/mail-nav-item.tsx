'use client';

// Copied from the licensed MUI Minimal template (next-ts src/sections/mail/mail-nav-item.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - the item is a link (`href`, Next Link, scroll kept) instead of an onClick label switch, with
//    aria-current on the selected item, because our rail switches a URL param (`?register=`);
//  - `label` is the page's { name, count, icon? } instead of IMailLabel; the icon is optional (a
//    register has no icon), the count renders as the template's caption figure even when 0;
//  - the button keeps the 44px touch floor on a phone; an optional trailing `action` (edit
//    IconButton) sits beside the button inside the same li.

import type { ReactNode } from 'react';
import type { IconifyName } from '@/components/minimal/iconify';

import Box from '@mui/material/Box';
import ListItemButton from '@mui/material/ListItemButton';

import Link from '@/components/no-prefetch-link';

import { Iconify } from '@/components/minimal/iconify';

// ----------------------------------------------------------------------

export type MailNavLabel = {
  name: string;
  count?: number | string;
  icon?: IconifyName;
  color?: string;
};

type Props = {
  selected: boolean;
  label: MailNavLabel;
  href: string;
  action?: ReactNode;
  /** Indent level (nested lists). */
  depth?: number;
};

export function MailNavItem({ selected, label, href, action, depth = 0 }: Props) {
  return (
    <Box component="li" sx={{ display: 'flex', alignItems: 'center', gap: 0.5, pl: depth * 2 }}>
      <ListItemButton
        disableGutters
        component={Link}
        href={href}
        scroll={false}
        aria-current={selected ? 'page' : undefined}
        sx={{
          pl: 1,
          pr: 1.5,
          gap: 2,
          minHeight: 44,
          borderRadius: 0.75,
          color: 'text.secondary',
          ...(selected && { color: 'text.primary' }),
        }}
      >
        {label.icon ? <Iconify icon={label.icon} width={22} sx={{ color: label.color }} /> : null}

        <Box
          component="span"
          sx={{
            flexGrow: 1,
            typography: selected ? 'subtitle2' : 'body2',
          }}
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
