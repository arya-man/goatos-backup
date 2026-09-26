// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/e-commerce/ecommerce-sales-overview.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - each row's figure is pre-formatted by the page (`display`) instead of fCurrency, because a
//    row may be a head count rather than rupees;
//  - an optional `caption` line under the bar (e.g. where a band's weights came from) and a
//    `color` per row instead of the template's match on its demo labels;
//  - `children` render at the foot of the card (a footnote the section owes its reader).

import type { CardProps } from '@mui/material/Card';
import type { LinearProgressProps } from '@mui/material/LinearProgress';

import { varAlpha } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import CardHeader from '@mui/material/CardHeader';
import LinearProgress from '@mui/material/LinearProgress';

import { fPercent } from '@/components/minimal/_shared/format-number';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  data: {
    label: string;
    value: number;
    display: string;
    caption?: string;
    color?: LinearProgressProps['color'];
  }[];
};

export function EcommerceSalesOverview({ title, subheader, data, sx, children, ...other }: Props) {
  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} />

      <Box
        sx={{
          gap: 4,
          px: 3,
          py: 4,
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        {data.map((progress) => (
          <Item key={progress.label} progress={progress} />
        ))}

        {children}
      </Box>
    </Card>
  );
}

// ----------------------------------------------------------------------

type ItemProps = {
  progress: Props['data'][number];
};

function Item({ progress }: ItemProps) {
  const color: LinearProgressProps['color'] = progress.color ?? 'primary';

  return (
    <div>
      <Box
        sx={{
          mb: 1,
          gap: 0.5,
          display: 'flex',
          alignItems: 'center',
          typography: 'subtitle2',
        }}
      >
        <Box component="span" sx={{ flexGrow: 1 }}>
          {progress.label}
        </Box>

        <Box component="span">{progress.display}</Box>

        <Box component="span" sx={{ typography: 'body2', color: 'text.secondary' }}>
          ({fPercent(progress.value)})
        </Box>
      </Box>

      <LinearProgress
        color={color}
        variant="determinate"
        value={progress.value}
        aria-label={progress.label}
        sx={[
          (theme) => ({
            height: 8,
            bgcolor: varAlpha(theme.vars.palette.grey['500Channel'], 0.16),
          }),
        ]}
      />

      {progress.caption ? (
        <Box component="div" sx={{ mt: 1, typography: 'caption', color: 'text.secondary' }}>
          {progress.caption}
        </Box>
      ) : null}
    </div>
  );
}
