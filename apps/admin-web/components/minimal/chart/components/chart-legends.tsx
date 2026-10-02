'use client';

import { mergeClasses } from 'minimal-shared/utils';

import { styled } from '@mui/material/styles';

import { chartClasses } from '../classes';

// ----------------------------------------------------------------------

export type ChartLegendsProps = React.ComponentProps<typeof ListRoot> & {
  labels?: string[];
  colors?: string[];
  values?: string[];
  sublabels?: string[];
  icons?: React.ReactNode[];
  slotProps?: {
    wrapper?: React.ComponentProps<typeof ItemWrapper>;
    root?: React.ComponentProps<typeof ItemRoot>;
    dot?: React.ComponentProps<typeof ItemDot>;
    icon?: React.ComponentProps<typeof ItemIcon>;
    value?: React.ComponentProps<typeof ItemValue>;
    label?: React.ComponentProps<typeof ItemLabel>;
  };
};

export function ChartLegends({
  sx,
  className,
  slotProps,
  icons = [],
  values = [],
  labels = [],
  colors = [],
  sublabels = [],
  ...other
}: ChartLegendsProps) {
  return (
    <ListRoot className={mergeClasses([chartClasses.legends.root, className])} sx={sx} {...other}>
      {labels.map((series, index) => (
        <ItemWrapper
          key={series}
          className={chartClasses.legends.item.wrapper}
          sx={[
            {
              '--icon-color': colors[index],
              ...slotProps?.wrapper,
            },
            ...(Array.isArray(slotProps?.wrapper?.sx)
              ? slotProps.wrapper.sx
              : [slotProps?.wrapper?.sx]),
          ]}
        >
          <ItemRoot className={chartClasses.legends.item.root} {...slotProps?.root}>
            {icons.length ? (
              <ItemIcon className={chartClasses.legends.item.icon} {...slotProps?.icon}>
                {icons[index]}
              </ItemIcon>
            ) : (
              <ItemDot className={chartClasses.legends.item.dot} {...slotProps?.dot} />
            )}

            <ItemLabel className={chartClasses.legends.item.label} {...slotProps?.label}>
              {series}
              {!!sublabels.length && <> {` (${sublabels[index]})`}</>}
            </ItemLabel>
          </ItemRoot>

          {values && (
            <ItemValue className={chartClasses.legends.item.value} {...slotProps?.value}>
              {values[index]}
            </ItemValue>
          )}
        </ItemWrapper>
      ))}
    </ListRoot>
  );
}

// ----------------------------------------------------------------------

const ListRoot = styled('ul')(({ theme }) => ({
  display: 'flex',
  flexWrap: 'wrap',
  gap: theme.spacing(2),
}));

// An item never paints outside its own cell. The label used to be `flexShrink: 0` in an
// unconstrained inline-flex, so in a two-column legend a long name ("Fattening animals · Female
// (₹20,76,766)") ran over the next item and off the card (PR #294 B2). Items are capped at their
// cell / the row, and a long label WRAPS (whole text kept, nothing truncated from either end).
const ItemWrapper = styled('li')(() => ({
  display: 'inline-flex',
  flexDirection: 'column',
  minWidth: 0,
  maxWidth: '100%',
}));

const ItemRoot = styled('div')(({ theme }) => ({
  gap: 6,
  minWidth: 0,
  maxWidth: '100%',
  alignItems: 'center',
  display: 'inline-flex',
  justifyContent: 'flex-start',
  fontSize: theme.typography.pxToRem(13),
  fontWeight: theme.typography.fontWeightMedium,
}));

const ItemIcon = styled('span')({
  display: 'inline-flex',
  color: 'var(--icon-color)',
  /**
   * As ':first-child' for ssr
   * https://github.com/emotion-js/emotion/issues/1105#issuecomment-1126025608
   */
  '& > :first-of-type:not(style):not(:first-of-type ~ *), & > style + *': {
    width: 20,
    height: 20,
  },
});

const ItemDot = styled('span')({
  width: 12,
  height: 12,
  flexShrink: 0,
  display: 'flex',
  borderRadius: '50%',
  position: 'relative',
  alignItems: 'center',
  justifyContent: 'center',
  color: 'var(--icon-color)',
  backgroundColor: 'currentColor',
});

const ItemLabel = styled('span')({ minWidth: 0, overflowWrap: 'anywhere' });

const ItemValue = styled('span')(({ theme }) => ({
  ...theme.typography.h6,
  marginTop: theme.spacing(1),
}));
