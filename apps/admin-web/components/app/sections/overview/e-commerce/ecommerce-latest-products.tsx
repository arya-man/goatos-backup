'use client';

// Template-derived (docs/design/template-derived.json): next-ts
// src/sections/overview/e-commerce/ecommerce-latest-products.tsx. Demo wiring as props (anatomy
// guarded): fCurrency(price) / fCurrency(priceSale) -> page-formatted display / priceSale (a page-formatted node); the
// avatar shows the item's initial when there is no cover photo; colours optional.
import type { BoxProps } from '@mui/material/Box';
import type { CardProps } from '@mui/material/Card';
import type { SxProps, Theme } from '@mui/material/styles';

import Box from '@mui/material/Box';
import Link from '@mui/material/Link';
import Card from '@mui/material/Card';
import Avatar from '@mui/material/Avatar';
import CardHeader from '@mui/material/CardHeader';

import { mergeSx } from '@/components/app/merge-sx';
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
    priceSale?: React.ReactNode;
    colors?: string[];
  }[];
  /** Declared overrides (template-derived.json): phone webview fit of the scroller and list. */
  slotProps?: { scrollbar?: SxProps<Theme>; list?: SxProps<Theme> };
  /** Declared override: names wrap instead of the template's one-line noWrap. */
  wrapNames?: boolean;
};

export function EcommerceLatestProducts({ title, subheader, list, slotProps, wrapNames, sx, ...other }: Props) {
  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} />

      <Scrollbar sx={mergeSx({ minHeight: 384 }, slotProps?.scrollbar)}>
        <Box
          role="list"
          sx={mergeSx({
            p: 3,
            gap: 3,
            minWidth: 360,
            display: 'flex',
            flexDirection: 'column',
          }, slotProps?.list)}
        >
          {list.map((item) => (
            <Item key={item.id} item={item} wrapNames={wrapNames} role="listitem" />
          ))}
        </Box>
      </Scrollbar>
    </Card>
  );
}

// ----------------------------------------------------------------------

type ItemProps = BoxProps & {
  item: Props['list'][number];
  wrapNames?: boolean;
};

function Item({ item, wrapNames, sx, ...other }: ItemProps) {
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
        <Link noWrap={!wrapNames} sx={{ color: 'text.primary', typography: 'subtitle2' }}>
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
          {!!item.priceSale && (
            <Box component="span" sx={{ textDecoration: 'line-through' }}>
              {item.priceSale}
            </Box>
          )}

          <Box component="span" sx={{ color: item.priceSale ? 'error.main' : 'inherit' }}>
            {item.display}
          </Box>
        </Box>
      </Box>

      <ColorPreview limit={3} colors={item.colors ?? []} />
    </Box>
  );
}
