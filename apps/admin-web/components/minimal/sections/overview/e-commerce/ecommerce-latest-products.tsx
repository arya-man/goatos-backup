// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/e-commerce/ecommerce-latest-products.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - the price line is pre-formatted by the page (`display`, e.g. "₹422 per kg"); the demo sale
//    price and colour swatches have no counterpart in our data and are dropped;
//  - the rounded avatar shows the item's initial (no product photos) and the name is plain text,
//    not a dead link;
//  - a plain vertical list replaces the custom scroller (phone webview: no scroll trap), and
//    `empty` renders when the list is empty.

import type { BoxProps } from '@mui/material/Box';
import type { CardProps } from '@mui/material/Card';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Avatar from '@mui/material/Avatar';
import CardHeader from '@mui/material/CardHeader';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  empty?: React.ReactNode;
  list: {
    id: string;
    name: string;
    display: string;
  }[];
};

export function EcommerceLatestProducts({ title, subheader, list, empty, sx, ...other }: Props) {
  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} />

      {list.length === 0 ? (
        <Box sx={{ p: 3 }}>{empty}</Box>
      ) : (
        <Box
          component="ul"
          sx={{
            p: 3,
            m: 0,
            gap: 3,
            display: 'flex',
            flexDirection: 'column',
            listStyle: 'none',
          }}
        >
          {list.map((item) => (
            <Item key={item.id} item={item} />
          ))}
        </Box>
      )}
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
      component="li"
      sx={[{ gap: 2, display: 'flex', alignItems: 'center' }, ...(Array.isArray(sx) ? sx : [sx])]}
      {...other}
    >
      <Avatar variant="rounded" alt={item.name} sx={{ width: 48, height: 48, flexShrink: 0 }}>
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
        <Box component="span" sx={{ color: 'text.primary', typography: 'subtitle2', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {item.name}
        </Box>

        <Box
          sx={{
            gap: 0.5,
            display: 'flex',
            typography: 'body2',
            color: 'text.secondary',
          }}
        >
          <Box component="span">{item.display}</Box>
        </Box>
      </Box>
    </Box>
  );
}
