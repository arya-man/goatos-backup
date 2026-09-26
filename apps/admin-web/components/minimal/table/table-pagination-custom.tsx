'use client';

import type { Theme, SxProps } from '@mui/material/styles';
import type { TablePaginationProps } from '@mui/material/TablePagination';

import Box from '@mui/material/Box';
import Switch from '@mui/material/Switch';
import TablePagination from '@mui/material/TablePagination';
import FormControlLabel from '@mui/material/FormControlLabel';

// ----------------------------------------------------------------------

export type TablePaginationCustomProps = TablePaginationProps & {
  dense?: boolean;
  sx?: SxProps<Theme>;
  onChangeDense?: (event: React.ChangeEvent<HTMLInputElement>) => void;
};

export function TablePaginationCustom({
  sx,
  dense,
  onChangeDense,
  rowsPerPageOptions = [5, 10, 25],
  ...other
}: TablePaginationCustomProps) {
  return (
    <Box sx={[{ position: 'relative' }, ...(Array.isArray(sx) ? sx : [sx])]}>
      <TablePagination
        rowsPerPageOptions={rowsPerPageOptions}
        component="div"
        {...other}
        // Mesha: the toolbar wraps at phone width so the arrows never slide out of the card.
        // The theme pins the toolbar at 64px, so on phone it grows with the wrapped row (no nested scroller).
        sx={{
          borderTopColor: 'transparent',
          overflow: { xs: 'visible', sm: 'auto' },
          '& .MuiTablePagination-toolbar': { flexWrap: { xs: 'wrap', sm: 'nowrap' }, justifyContent: 'flex-end', rowGap: 0.5, height: { xs: 'auto', sm: 64 }, minHeight: 64 },
        }}
      />

      {onChangeDense && (
        <FormControlLabel
          label="Dense"
          control={
            <Switch
              checked={dense}
              onChange={onChangeDense}
              slotProps={{ input: { id: 'dense-switch' } }}
            />
          }
          sx={{
            pl: 2,
            py: 1.5,
            top: 0,
            position: { sm: 'absolute' },
          }}
        />
      )}
    </Box>
  );
}
