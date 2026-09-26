// Copied from the licensed MUI Minimal template (src/sections/user/user-table-row.tsx).
// Changes: the row takes its cells as props instead of an IUserItem (name link, secondary line,
// middle cells, status Label, trailing actions); the bulk-select checkbox, quick-edit form, popover
// menu and delete confirm are dropped (the directory has no bulk actions — the row opens a URL-driven
// drawer). Initials render in the Avatar when there is no photo. No hooks, so it renders on the server.
import type { ReactNode } from 'react';
import type { LabelColor } from '../../label';

import Box from '@mui/material/Box';
import Stack from '@mui/material/Stack';
import Avatar from '@mui/material/Avatar';
import TableRow from '@mui/material/TableRow';
import TableCell from '@mui/material/TableCell';

import { Label } from '../../label';

// ----------------------------------------------------------------------

export type UserTableRowProps = {
  /** Accessible name / initials source for the avatar. */
  name: string;
  /** The name cell's link (the template's RouterLink to the edit page). */
  nameLink: ReactNode;
  /** The muted line under the name (the template's email). */
  secondary?: ReactNode;
  avatarUrl?: string;
  cells: ReactNode[];
  status?: { label: ReactNode; color: LabelColor };
  extra?: ReactNode[];
  actions?: ReactNode;
};

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  return ((parts[0]?.[0] ?? '') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase();
}

export function UserTableRow({ name, nameLink, secondary, avatarUrl, cells, status, extra = [], actions }: UserTableRowProps) {
  return (
    <TableRow hover tabIndex={-1}>
      <TableCell>
        <Box sx={{ gap: 2, display: 'flex', alignItems: 'center' }}>
          <Avatar alt={name} src={avatarUrl}>
            {initials(name)}
          </Avatar>

          <Stack sx={{ typography: 'body2', flex: '1 1 auto', alignItems: 'flex-start', minWidth: 0 }}>
            {nameLink}
            {secondary ? (
              <Box component="span" sx={{ color: 'text.disabled' }}>
                {secondary}
              </Box>
            ) : null}
          </Stack>
        </Box>
      </TableCell>

      {cells.map((cell, index) => (
        <TableCell key={index} sx={{ whiteSpace: 'nowrap' }}>
          {cell}
        </TableCell>
      ))}

      {status ? (
        <TableCell>
          <Label variant="soft" color={status.color}>
            {status.label}
          </Label>
        </TableCell>
      ) : null}

      {extra.map((cell, index) => (
        <TableCell key={`extra-${index}`}>{cell}</TableCell>
      ))}

      {actions ? (
        <TableCell>
          <Box sx={{ display: 'flex', alignItems: 'center' }}>{actions}</Box>
        </TableCell>
      ) : null}
    </TableRow>
  );
}
