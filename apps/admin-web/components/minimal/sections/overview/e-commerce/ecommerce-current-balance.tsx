// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/e-commerce/ecommerce-current-balance.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - the big figure and every row are pre-formatted by the page (`total`, `rows`) instead of
//    fCurrency over three fixed demo rows, because a figure may be grams a day or a head count;
//  - the demo Request/Transfer buttons have no counterpart here and are dropped; `children`
//    render in their place (e.g. a caption the figure owes its reader).

import type { CardProps } from '@mui/material/Card';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title: React.ReactNode;
  total: React.ReactNode;
  rows: { label: React.ReactNode; value: React.ReactNode }[];
};

export function EcommerceCurrentBalance({ sx, title, total, rows, children, ...other }: Props) {
  const renderRow = (label: React.ReactNode, value: React.ReactNode, key: number) => (
    <Box key={key} sx={{ display: 'flex', typography: 'body2', justifyContent: 'space-between', gap: 2 }}>
      <Box component="span" sx={{ color: 'text.secondary' }}>
        {label}
      </Box>

      <Box component="span">{value}</Box>
    </Box>
  );

  return (
    <Card sx={[{ p: 3 }, ...(Array.isArray(sx) ? sx : [sx])]} {...other}>
      <Box sx={{ mb: 1, typography: 'subtitle2' }}>{title}</Box>

      <Box sx={{ gap: 2, display: 'flex', flexDirection: 'column' }}>
        <Box sx={{ typography: 'h3' }}>{total}</Box>

        {rows.map((row, index) => renderRow(row.label, row.value, index))}

        {children}
      </Box>
    </Card>
  );
}
