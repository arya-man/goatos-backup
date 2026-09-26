'use client';

// Copied from the licensed MUI Minimal template (next-ts src/sections/order/order-table-toolbar.tsx).
// Mesha changes (data plumbing only, anatomy untouched): the template's two DatePickers and the
// search TextField are the page's own filter controls (`filters` = the leading fixed-width fields,
// `search` = the growing field, both URL-backed on our pages), and the Print/Import/Export menu is
// the page's `menuActions` (Columns, Export CSV, …) in the same CustomPopover + MenuList behind the
// ⋮ IconButton. The Box layout (p 2.5, gap 2, column on xs, row on md) is the template's.
// Also used for user-table-toolbar.tsx / invoice-table-toolbar.tsx, which share this layout.

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
  /** Fixed-width leading controls (selects, date pickers). */
  filters?: ReactNode;
  /** The growing search field. */
  search?: ReactNode;
  /** Trailing buttons shown inline before the ⋮ menu (e.g. Apply). */
  trailing?: ReactNode;
  menuActions?: OrderToolbarMenuAction[];
  menuLabel?: string;
};

export function OrderTableToolbar({ filters, search, trailing, menuActions = [], menuLabel = 'More' }: Props) {
  const menu = usePopover();

  const renderMenuActions = () => (
    <CustomPopover
      open={menu.open}
      anchorEl={menu.anchorEl}
      onClose={menu.onClose}
      slotProps={{ arrow: { placement: 'right-top' } }}
    >
      <MenuList>
        {menuActions.map((action) => (
          <MenuItem
            key={action.key}
            onClick={() => {
              const anchor = menu.anchorEl;
              menu.onClose();
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
          // Mesha: with no search field the ⋮ stays on the filter's row on a phone instead of
          // taking a row of its own under it.
          flexDirection: { xs: search ? 'column' : 'row', md: 'row' },
          flexWrap: { md: 'wrap' },
          alignItems: { xs: search ? 'stretch' : 'center', md: 'center' },
          '& > .order-toolbar-filter': search
            ? { width: { xs: 1, md: 160 }, flex: { md: '0 0 auto' } }
            : { width: { md: 160 }, flex: { xs: '1 1 0', md: '0 0 auto' }, minWidth: 0 },
        }}
      >
        {filters}

        <Box
          sx={{
            gap: 2,
            width: { xs: search ? 1 : 'auto', md: 'auto' },
            flex: { xs: search ? 'initial' : '0 0 auto', md: search ? '1 1 320px' : '1 1 auto' },
            display: 'flex',
            flexWrap: { xs: 'wrap', md: 'nowrap' },
            alignItems: 'center',
          }}
        >
          {search ? <Box sx={{ flex: '1 1 240px', minWidth: 0 }}>{search}</Box> : <Box sx={{ flexGrow: 1 }} />}

          {trailing}

          {menuActions.length ? (
            <IconButton onClick={menu.onOpen} aria-label={menuLabel}>
              <Iconify icon="eva:more-vertical-fill" />
            </IconButton>
          ) : null}
        </Box>
      </Box>

      {menuActions.length ? renderMenuActions() : null}
    </>
  );
}
