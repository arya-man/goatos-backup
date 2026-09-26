import type { Theme, SxProps } from '@mui/material/styles';
import type { ButtonBaseProps } from '@mui/material/ButtonBase';
import type { IconifyProps } from '@/layouts/template/iconify';

import Box from '@mui/material/Box';
import ButtonBase from '@mui/material/ButtonBase';

import { Label } from '@/layouts/template/label';
import { Iconify } from '@/layouts/template/iconify';

// ----------------------------------------------------------------------

// Minimal v7.7.0 next-ts layouts/components/workspaces-popover.tsx, `renderButton()` only: the header
// workspace switcher trigger. Mesha feeds it the park scope (name + optional "all sheds" label) and
// owns the CustomPopover list in the shell; the workspace logo <img> is the park mark icon. Styles are
// the template's; the phone tap floor (44px below sm) is the admin-web WebView rule.

export type WorkspacesButtonProps = ButtonBaseProps & {
  open: boolean;
  name: string;
  plan?: string | null;
  icon?: IconifyProps['icon'];
};

export function WorkspacesButton({ open, name, plan, icon = 'mingcute:location-fill', sx, ...other }: WorkspacesButtonProps) {
  const mediaQuery = 'sm';

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

  return (
    <ButtonBase
      disableRipple
      sx={[
        {
          py: 0.5,
          gap: { xs: 0.5, [mediaQuery]: 1 },
          minHeight: { xs: 44, [mediaQuery]: 'auto' },
          '&::before': buttonBg,
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      <Iconify width={24} icon={icon} sx={{ color: 'primary.main' }} />

      <Box
        component="span"
        sx={{ typography: 'subtitle2', display: { xs: 'none', [mediaQuery]: 'inline-flex' } }}
      >
        {name}
      </Box>

      {plan ? (
        <Label
          color="default"
          sx={{
            height: 22,
            cursor: 'inherit',
            display: { xs: 'none', [mediaQuery]: 'inline-flex' },
          }}
        >
          {plan}
        </Label>
      ) : null}

      <Iconify width={16} icon="carbon:chevron-sort" sx={{ color: 'text.disabled' }} />
    </ButtonBase>
  );
}
