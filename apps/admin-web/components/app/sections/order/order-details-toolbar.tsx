'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/order/order-details-toolbar.tsx. Anatomy guarded; the demo order becomes props:
// `title` (its "Order {orderNumber}", rendered as the page h1), `status` + `statusColor` (its status
// Label colour match), `subtitle` (its fDateTime line), the status menu via optional
// `statusOptions` / `onChangeStatus` (as the template), and the `actions` SLOT (its Print / Edit
// buttons). The back IconButton renders when there is a `backHref` and carries an aria-label.
import type { ReactNode } from 'react';
import type { LabelColor } from '@/components/minimal/label';
import type { SxProps, Theme } from '@mui/material/styles';

import { usePopover } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import Stack from '@mui/material/Stack';
import Button from '@mui/material/Button';
import MenuList from '@mui/material/MenuList';
import MenuItem from '@mui/material/MenuItem';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';

import { mergeSx } from '@/components/app/merge-sx';
import { RouterLink } from '@/layouts/template/routes/components';

import { Label } from '@/components/minimal/label';
import { Iconify } from '@/components/minimal/iconify';
import { CustomPopover } from '@/components/minimal/custom-popover';

// ----------------------------------------------------------------------

type Props = {
  title: ReactNode;
  status?: ReactNode;
  statusColor?: LabelColor;
  backHref?: string;
  backLabel?: string;
  subtitle?: ReactNode;
  statusOptions?: { value: string; label: string }[];
  statusValue?: string;
  onChangeStatus?: (newValue: string) => void;
  actions?: ReactNode;
  /** Declared override (template-derived.json): merged after the template's action-row sx (wrap on a phone). */
  slotProps?: { actions?: SxProps<Theme>; statusButton?: SxProps<Theme> };
};

const HEADING = 'h1';

export function OrderDetailsToolbar({
  title,
  status,
  statusColor = 'default',
  backHref,
  backLabel = 'Back',
  subtitle,
  statusOptions = [],
  statusValue,
  onChangeStatus,
  actions,
  slotProps,
}: Props) {
  const menuActions = usePopover();

  const renderMenuActions = () => (
    <CustomPopover
      open={menuActions.open}
      anchorEl={menuActions.anchorEl}
      onClose={menuActions.onClose}
      slotProps={{ arrow: { placement: 'top-right' } }}
    >
      <MenuList>
        {statusOptions.map((option) => (
          <MenuItem
            key={option.value}
            selected={option.value === statusValue}
            onClick={() => {
              menuActions.onClose();
              onChangeStatus?.(option.value);
            }}
          >
            {option.label}
          </MenuItem>
        ))}
      </MenuList>
    </CustomPopover>
  );

  return (
    <>
      <Box
        sx={{
          gap: 3,
          display: 'flex',
          mb: { xs: 3, md: 5 },
          flexDirection: { xs: 'column', md: 'row' },
        }}
      >
        <Box sx={{ gap: 1, display: 'flex', alignItems: 'flex-start' }}>
          {backHref ? (
          <IconButton component={RouterLink} href={backHref} aria-label={backLabel}>
            <Iconify icon="eva:arrow-ios-back-fill" />
          </IconButton>
          ) : null}

          <Stack spacing={0.5}>
            <Box sx={{ gap: 1, display: 'flex', alignItems: 'center' }}>
              <Typography variant="h4" component={HEADING}>{title}</Typography>
              {status ? (
              <Label
                variant="soft"
                color={statusColor}
              >
                {status}
              </Label>
              ) : null}
            </Box>

            <Typography variant="body2" sx={{ color: 'text.disabled' }}>
              {subtitle}
            </Typography>
          </Stack>
        </Box>

        <Box
          sx={mergeSx({
            gap: 1.5,
            flexGrow: 1,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'flex-end',
          }, slotProps?.actions)}
        >
          {statusOptions.length ? (
          <Button
            color="inherit"
            variant="outlined"
            endIcon={<Iconify icon="eva:arrow-ios-downward-fill" />}
            onClick={menuActions.onOpen}
            sx={mergeSx({ textTransform: 'capitalize' }, slotProps?.statusButton)}
          >
            {status}
          </Button>
          ) : null}

          {actions}
        </Box>
      </Box>

      {renderMenuActions()}
    </>
  );
}
