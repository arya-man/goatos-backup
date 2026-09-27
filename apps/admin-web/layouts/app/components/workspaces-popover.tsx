'use client';

import type { Theme, SxProps } from '@mui/material/styles';
import type { MenuListProps } from '@mui/material/MenuList';
import type { ButtonBaseProps } from '@mui/material/ButtonBase';

import { useCallback } from 'react';
import { usePopover } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import Avatar from '@mui/material/Avatar';
import MenuList from '@mui/material/MenuList';
import MenuItem from '@mui/material/MenuItem';
import Typography from '@mui/material/Typography';
import ButtonBase from '@mui/material/ButtonBase';

import { Label } from '@/layouts/template/label';
import { Iconify } from '@/layouts/template/iconify';
import { Scrollbar } from '@/layouts/template/scrollbar';
import { RouterLink } from '@/layouts/template/routes/components';
import { CustomPopover } from '@/layouts/template/custom-popover';

import { pickScopeOption } from '@/lib/scope';

// ----------------------------------------------------------------------

// Template-derived copy of Minimal v7.7.0 next-ts src/layouts/components/workspaces-popover.tsx (anatomy
// pinned in docs/design/template-derived.json): the header park-scope switcher. The shell owns the
// selection (value = the scope in the URL) and each option is a link (href, history replace) instead of
// the template's local demo state; the "Create workspace" demo button is not carried (bottomArea slot
// in its place); slotProps.menuList carries the listbox role, label and subheader. Declared overrides
// (manifest `replaced`): an option name wraps instead of noWrap and shows the park code as a caption
// line under it; rows grow from a 48px floor (the xs key outranks MenuItem's own sm minHeight auto);
// a value outside `data` shows `fallback` on the trigger and selects no option (pickScopeOption).

export type WorkspacesPopoverProps = ButtonBaseProps & {
  data?: {
    id: string;
    name: string;
    logo: string;
    plan: string;
    href: string;
    code?: string | null;
  }[];
  value?: string;
  fallback?: { name: string; logo: string; plan: string };
  slots?: { bottomArea?: React.ReactNode };
  slotProps?: { menuList?: MenuListProps };
};

export function WorkspacesPopover({
  data = [],
  value,
  fallback,
  slots,
  slotProps,
  sx,
  ...other
}: WorkspacesPopoverProps) {
  const mediaQuery = 'sm';

  const { open, anchorEl, onClose, onOpen } = usePopover();

  const { current: workspace, selectedId } = pickScopeOption(data, value, fallback);

  const handleChangeWorkspace = useCallback(() => {
    onClose();
  }, [onClose]);

  const buttonBg: SxProps<Theme> = {
    height: 1,
    zIndex: -1,
    opacity: 0,
    content: "''",
    borderRadius: 1,
    position: 'absolute',
    visibility: 'hidden',
    bgcolor: 'action.hover',
    width: 'calc(100% + 8px)',
    transition: (theme) =>
      theme.transitions.create(['opacity', 'visibility'], {
        easing: theme.transitions.easing.sharp,
        duration: theme.transitions.duration.shorter,
      }),
    ...(open && {
      opacity: 1,
      visibility: 'visible',
    }),
  };

  const renderButton = () => (
    <ButtonBase
      disableRipple
      onClick={onOpen}
      aria-haspopup="listbox"
      aria-expanded={open}
      sx={[
        {
          py: 0.5,
          gap: { xs: 0.5, [mediaQuery]: 1 },
          '&::before': buttonBg,
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      <Box
        component="img"
        alt={workspace?.name}
        src={workspace?.logo}
        sx={{ width: 24, height: 24, borderRadius: '50%' }}
      />

      <Box
        component="span"
        sx={{ typography: 'subtitle2', display: { xs: 'none', [mediaQuery]: 'inline-flex' } }}
      >
        {workspace?.name}
      </Box>

      <Label
        color={workspace?.plan === 'Free' ? 'default' : 'info'}
        sx={{
          height: 22,
          cursor: 'inherit',
          display: { xs: 'none', [mediaQuery]: 'inline-flex' },
        }}
      >
        {workspace?.plan}
      </Label>

      <Iconify width={16} icon="carbon:chevron-sort" sx={{ color: 'text.disabled' }} />
    </ButtonBase>
  );

  const renderMenuList = () => (
    <CustomPopover
      open={open}
      anchorEl={anchorEl}
      onClose={onClose}
      slotProps={{
        arrow: { placement: 'top-left' },
        paper: { sx: { mt: 0.5, ml: -1.55, width: 240 } },
      }}
    >
      <Scrollbar sx={{ maxHeight: 240 }}>
        <MenuList {...slotProps?.menuList}>
          {data.map((option) => (
            <MenuItem
              key={option.id}
              component={RouterLink}
              href={option.href}
              replace
              scroll={false}
              role="option"
              aria-selected={option.id === selectedId}
              selected={option.id === selectedId}
              onClick={() => handleChangeWorkspace()}
              sx={{ minHeight: { xs: 48 }, height: 'auto' }}
            >
              <Avatar alt={option.name} src={option.logo} sx={{ width: 24, height: 24 }} />

              <Typography
                component="span"
                variant="body2"
                sx={{ flexGrow: 1, fontWeight: 'fontWeightMedium', minWidth: 0, whiteSpace: 'normal', overflowWrap: 'anywhere' }}
              >
                {option.name}
                {option.code ? (
                  <Box component="span" sx={{ display: 'block', typography: 'caption', color: 'text.secondary' }}>
                    {option.code}
                  </Box>
                ) : null}
              </Typography>

              <Label color={option.plan === 'Free' ? 'default' : 'info'}>{option.plan}</Label>
            </MenuItem>
          ))}
        </MenuList>
      </Scrollbar>

      {slots?.bottomArea}
    </CustomPopover>
  );

  return (
    <>
      {renderButton()}
      {renderMenuList()}
    </>
  );
}
