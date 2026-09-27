'use client';

// Template-derived (docs/design/template-derived.json): next-ts
// src/sections/overview/e-commerce/ecommerce-latest-products.tsx. Demo wiring as props (anatomy
// guarded): fCurrency(price) / fCurrency(priceSale) -> page-formatted display / displaySale; the
// avatar shows the item's initial when there is no cover photo; colours optional.
import type { BoxProps } from '@mui/material/Box';
import type { CardProps } from '@mui/material/Card';

import Box from '@mui/material/Box';
import Link from '@mui/material/Link';
import Card from '@mui/material/Card';
import Avatar from '@mui/material/Avatar';
import CardHeader from '@mui/material/CardHeader';

import { Scrollbar } from '@/components/minimal/scrollbar';
import { ColorPreview } from '@/components/minimal/color-utils';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  list: {
    id: string;
    name: string;
    coverUrl?: string;
    /** The page-formatted figure (the template prints fCurrency(price)). */
    display: React.ReactNode;
    /** Optional struck-through earlier figure (the template's sale price). */
    displaySale?: React.ReactNode;
    colors?: string[];
  }[];
};

export function EcommerceLatestProducts({ title, subheader, list, sx, ...other }: Props) {
  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} />

      <Scrollbar sx={{ minHeight: 384 }}>
        <Box
          sx={{
            p: 3,
            gap: 3,
            minWidth: 360,
            display: 'flex',
            flexDirection: 'column',
          }}
        >
          {list.map((item) => (
            <Item key={item.id} item={item} />
          ))}
        </Box>
      </Scrollbar>
    </Card>
  );
}

// ----------------------------------------------------------------------

type ItemProps = BoxProps & {
  item: Props['list'][number];
};

function Item({ item, sx, ...other }: ItemProps) {
  return (
    <Box
      sx={[{ gap: 2, display: 'flex', alignItems: 'center' }, ...(Array.isArray(sx) ? sx : [sx])]}
      {...other}
    >
      <Avatar
        variant="rounded"
        alt={item.name}
        src={item.coverUrl}
        sx={{ width: 48, height: 48, flexShrink: 0 }}
      >
        {item.name.trim().charAt(0).toUpperCase() || '?'}
      </Avatar>

      <Box
        sx={{
          gap: 0.5,
          minWidth: 0,
          display: 'flex',
          flex: '1 1 auto',
          flexDirection: 'column',
        }}
      >
        <Link noWrap sx={{ color: 'text.primary', typography: 'subtitle2' }}>
          {item.name}
        </Link>

        <Box
          sx={{
            gap: 0.5,
            display: 'flex',
            typography: 'body2',
            color: 'text.secondary',
          }}
        >
          {!!item.displaySale && (
            <Box component="span" sx={{ textDecoration: 'line-through' }}>
              {item.displaySale}
            </Box>
          )}

          <Box component="span" sx={{ color: item.displaySale ? 'error.main' : 'inherit' }}>
            {item.display}
          </Box>
        </Box>
      </Box>

      <ColorPreview limit={3} colors={item.colors ?? []} />
    </Box>
  );
}
