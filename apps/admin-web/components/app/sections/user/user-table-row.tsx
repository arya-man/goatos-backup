'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/user/user-table-row.tsx. The row anatomy is the template's (checkbox cell, Avatar +
// name link + muted line, status Label, actions cell with the ⋮ IconButton + CustomPopover); SLOTS and
// props replace the demo model (IUserItem) and demo flows:
//  - `name` / `nameHref` / `nameLinkComponent` / `secondary` / `avatarUrl`: the identity cell;
//  - `cells`: the middle nowrap cells (the template's phone / company / role);
//  - `status`: the Label; `extra`: cells after it;
//  - `actions`: the row's own action (the template's Quick edit IconButton);
//  - `menuActions`: the ⋮ items (the template's Edit / Delete, whose quick-edit form and delete
//    confirm are demo flows and not carried);
//  - `selected` / `onSelectRow`: the checkbox cell renders only when the page selects rows.
import type { ReactNode, ElementType } from 'react';
import type { LabelColor } from '@/components/minimal/label';

import { usePopover } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import Link from '@mui/material/Link';
import Stack from '@mui/material/Stack';
import Avatar from '@mui/material/Avatar';
import MenuList from '@mui/material/MenuList';
import MenuItem from '@mui/material/MenuItem';
import TableRow from '@mui/material/TableRow';
import Checkbox from '@mui/material/Checkbox';
import TableCell from '@mui/material/TableCell';
import IconButton from '@mui/material/IconButton';

import { RouterLink } from '@/layouts/template/routes/components';

import { Label } from '@/components/minimal/label';
import { Iconify } from '@/components/minimal/iconify';
import { CustomPopover } from '@/components/minimal/custom-popover';

// ----------------------------------------------------------------------

export type UserRowMenuAction = { key: string; label: ReactNode; icon?: ReactNode; onClick: () => void; color?: string };

type Props = {
  id: string;
  name: string;
  nameHref: string;
  nameLinkComponent?: ElementType;
  /** Extra props for the name link (e.g. scroll={false} on a same-page overlay link). */
  nameLinkProps?: Record<string, unknown>;
  secondary?: ReactNode;
  avatarUrl?: string;
  cells: ReactNode[];
  status?: { label: ReactNode; color: LabelColor };
  extra?: ReactNode[];
  actions?: ReactNode;
  menuActions?: UserRowMenuAction[];
  selected?: boolean;
  onSelectRow?: () => void;
};

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  return ((parts[0]?.[0] ?? '') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase();
}

export function UserTableRow({ id, name, nameHref, nameLinkComponent, nameLinkProps, secondary, avatarUrl, cells, status, extra = [], actions, menuActions = [], selected = false, onSelectRow }: Props) {
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
            sx={action.color ? { color: action.color } : undefined}
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
      <TableRow hover selected={selected} aria-checked={selected} tabIndex={-1}>
        {onSelectRow ? (
          <TableCell padding="checkbox">
            <Checkbox
              checked={selected}
              onClick={onSelectRow}
              slotProps={{
                input: {
                  id: `${id}-checkbox`,
                  'aria-label': `${id} checkbox`,
                },
              }}
            />
          </TableCell>
        ) : null}

        <TableCell>
          <Box sx={{ gap: 2, display: 'flex', alignItems: 'center' }}>
            <Avatar alt={name} src={avatarUrl}>
              {initials(name)}
            </Avatar>

            <Stack sx={{ typography: 'body2', flex: '1 1 auto', alignItems: 'flex-start' }}>
              <Link
                component={nameLinkComponent ?? RouterLink}
                href={nameHref}
                color="inherit"
                sx={{ cursor: 'pointer' }}
                {...nameLinkProps}
              >
                {name}
              </Link>
              <Box component="span" sx={{ color: 'text.disabled' }}>
                {secondary}
              </Box>
            </Stack>
          </Box>
        </TableCell>

        {cells.map((cell, index) => (
          <TableCell key={index} sx={{ whiteSpace: 'nowrap' }}>{cell}</TableCell>
        ))}

        <TableCell>
          {status ? (
            <Label variant="soft" color={status.color}>
              {status.label}
            </Label>
          ) : null}
        </TableCell>

        {extra.map((cell, index) => (
          <TableCell key={`extra-${index}`}>{cell}</TableCell>
        ))}

        <TableCell>
          <Box sx={{ display: 'flex', alignItems: 'center' }}>
            {actions}

            {menuActions.length ? (
              <IconButton
                color={menuActionsPopover.open ? 'inherit' : 'default'}
                onClick={menuActionsPopover.onOpen}
              >
                <Iconify icon="eva:more-vertical-fill" />
              </IconButton>
            ) : null}
          </Box>
        </TableCell>
      </TableRow>

      {renderMenuActions()}
    </>
  );
}
