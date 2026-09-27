'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/order/order-table-toolbar.tsx. SLOTS replace the template's demo controls; everything
// else (the Box layouts, the ⋮ IconButton + CustomPopover + MenuList) is the template's
// (guard: template-derived-anatomy):
//  - `filters`: the leading controls (the template's Start / End DatePickers); give each element
//    `orderToolbarFilterSx` (components/app/order-toolbar-filter) for the template's field width;
//  - `search`: the growing field (the template's fullWidth search TextField; pass it fullWidth);
//  - `trailing`: buttons that act on the filters (e.g. Apply), right after them: their own row on a
//    phone (the template column stacks) so the search keeps its width;
//  - `menuActions`: the ⋮ menu items (the template's demo Print / Import / Export).
import type { ReactNode } from 'react';

import { usePopover } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import MenuList from '@mui/material/MenuList';
import MenuItem from '@mui/material/MenuItem';
import IconButton from '@mui/material/IconButton';

import { Iconify } from '@/components/minimal/iconify';
import { CustomPopover } from '@/components/minimal/custom-popover';

// ----------------------------------------------------------------------

export type OrderToolbarMenuAction = {
  key: string;
  label: ReactNode;
  icon?: ReactNode;
  /** Gets the ⋮ button, so an action can open its own popover (e.g. Columns) in the same place. */
  onClick: (anchor: HTMLElement | null) => void;
};

type Props = {
  filters?: ReactNode;
  search?: ReactNode;
  trailing?: ReactNode;
  menuActions?: OrderToolbarMenuAction[];
  menuLabel?: string;
};

export function OrderTableToolbar({ filters, search, trailing, menuActions = [], menuLabel = 'More' }: Props) {
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
              const anchor = menuActionsPopover.anchorEl;
              menuActionsPopover.onClose();
              action.onClick(anchor);
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
      <Box
        sx={{
          p: 2.5,
          gap: 2,
          display: 'flex',
          pr: { xs: 2.5, md: 1 },
          flexDirection: { xs: 'column', md: 'row' },
          alignItems: { xs: 'flex-end', md: 'center' },
        }}
      >
        {filters}

        {trailing}

        <Box
          sx={{
            gap: 2,
            width: 1,
            flexGrow: 1,
            display: 'flex',
            alignItems: 'center',
          }}
        >
          {search ?? <Box sx={{ flexGrow: 1 }} />}

          <IconButton onClick={menuActionsPopover.onOpen} aria-label={menuLabel} disabled={!menuActions.length}>
            <Iconify icon="eva:more-vertical-fill" />
          </IconButton>
        </Box>
      </Box>

      {renderMenuActions()}
    </>
  );
}
