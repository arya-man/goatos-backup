// Ranked table card (components/app adapter, NOT a template file): the template overview table card
// anatomy (EcommerceBestSalesman: Card, CardHeader, TableHeadCustom, avatar + name lead cell, soft
// rank Label) composed from MUI + template parts for a table of ANY width. The template section
// itself has a fixed five-column demo row (email, flag, fCurrency, 'Top N'), so it is not used.
//  - rows are the page's own cells (`cells`, aligned with `headCells`) behind the template's
//    avatar + name lead cell, because the demo row shape (email, flag, rank) is not ours; a
//    trailing Label cell keeps the template's soft rank chip;
//  - the table sits in a plain sideways-scroll box (phone webview: no custom scroller) with its
//    own accessible name, and `children` render under it (the list's pager);
//  - the demo `minHeight` is dropped so a short list does not leave an empty well.

import type { CardProps } from '@mui/material/Card';
import type { LabelColor } from '@/layouts/template/label/types';
import type { TableHeadCellProps } from '@/components/app/table';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Table from '@mui/material/Table';
import Avatar from '@mui/material/Avatar';
import TableRow from '@mui/material/TableRow';
import TableCell from '@mui/material/TableCell';
import TableBody from '@mui/material/TableBody';
import CardHeader from '@mui/material/CardHeader';

import { Label } from '@/components/minimal/label';
import { TableHeadCustom } from '@/components/app/table';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  action?: React.ReactNode;
  headCells: TableHeadCellProps[];
  /** Accessible name of the table and its scroll region. */
  tableLabel: string;
  /** id of the scroll region (the pager's density toggle targets it). */
  regionId?: string;
  tableData: {
    id: string;
    name: string;
    secondary?: string;
    /** Cells after the lead cell, in `headCells` order (minus the first and, with `rank`, the last). */
    cells: { value: React.ReactNode; align?: 'left' | 'center' | 'right' }[];
    rank?: { label: string; color: LabelColor };
  }[];
};

export function RankedTableCard({
  title,
  subheader,
  action,
  tableData,
  headCells,
  tableLabel,
  regionId,
  sx,
  children,
  ...other
}: Props) {
  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} action={action} sx={{ mb: 3 }} />

      <Box id={regionId} tabIndex={0} role="region" aria-label={tableLabel} sx={{ overflowX: 'auto', maxWidth: '100%' }}>
        <Table sx={{ minWidth: 640 }} aria-label={tableLabel}>
          <TableHeadCustom headCells={headCells} />

          <TableBody>
            {tableData.map((row) => (
              <RowItem key={row.id} row={row} />
            ))}
          </TableBody>
        </Table>
      </Box>

      {children}
    </Card>
  );
}

// ----------------------------------------------------------------------

type RowItemProps = {
  row: Props['tableData'][number];
};

function RowItem({ row }: RowItemProps) {
  return (
    <TableRow>
      <TableCell>
        <Box sx={{ gap: 2, display: 'flex', alignItems: 'center' }}>
          <Avatar alt={row.name}>{row.name.trim().charAt(0).toUpperCase() || '?'}</Avatar>
          <Box sx={{ minWidth: 0, display: 'flex', flexDirection: 'column' }}>
            <Box component="span" sx={{ typography: 'subtitle2', whiteSpace: 'nowrap' }}>
              {row.name}
            </Box>
            {row.secondary ? (
              <Box component="span" sx={{ typography: 'body2', color: 'text.secondary', whiteSpace: 'nowrap' }}>
                {row.secondary}
              </Box>
            ) : null}
          </Box>
        </Box>
      </TableCell>

      {row.cells.map((cell, index) => (
        <TableCell key={index} align={cell.align} sx={{ whiteSpace: 'nowrap' }}>
          {cell.value}
        </TableCell>
      ))}

      {row.rank ? (
        <TableCell align="right">
          <Label variant="soft" color={row.rank.color}>
            {row.rank.label}
          </Label>
        </TableCell>
      ) : null}
    </TableRow>
  );
}
