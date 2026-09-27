"use client";

import type { NavSectionProps } from '@/layouts/template/nav-section';

import { useEffect } from 'react';
import { mergeClasses } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';

import { usePathname } from '@/layouts/template/routes/hooks';
import { useBackCloses } from '@/components/use-back-closes';

import { Logo } from '@/layouts/template/logo';
import { Scrollbar } from '@/layouts/template/scrollbar';
import { NavSectionVertical } from '@/layouts/template/nav-section';

import { layoutClasses } from '@/layouts/core';

// ----------------------------------------------------------------------

type NavMobileProps = NavSectionProps & {
  open: boolean;
  onClose: () => void;
  slots?: {
    topArea?: React.ReactNode;
    bottomArea?: React.ReactNode;
  };
};

export function NavMobile({
  sx,
  data,
  open,
  slots,
  onClose,
  className,
  checkPermissions,
  ...other
}: NavMobileProps) {
  const pathname = usePathname();

  // Mesha: Android Back closes the phone menu instead of leaving the page (house drawer rule).
  useBackCloses(open, onClose);

  useEffect(() => {
    if (open) {
      onClose();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname]);

  return (
    <Drawer
      open={open}
      onClose={onClose}
      slotProps={{
        paper: {
          className: mergeClasses([layoutClasses.nav.root, layoutClasses.nav.vertical, className]),
          sx: [
            {
              overflow: 'unset',
              bgcolor: 'var(--layout-nav-bg)',
              width: 'var(--layout-nav-mobile-width)',
              // Mesha WebView: keep the drawer clear of notches / gesture bars (viewport-fit=cover).
              pt: 'env(safe-area-inset-top, 0px)',
              pb: 'env(safe-area-inset-bottom, 0px)',
              pl: 'env(safe-area-inset-left, 0px)',
            },
            ...(Array.isArray(sx) ? sx : [sx]),
          ],
        },
      }}
    >
      {slots?.topArea ?? (
        <Box sx={{ pl: 3.5, pt: 2.5, pb: 1 }}>
          <Logo />
        </Box>
      )}

      <Scrollbar fillContent>
        <NavSectionVertical
          data={data}
          checkPermissions={checkPermissions}
          sx={{ px: 2, flex: '1 1 auto' }}
          {...other}
        />
      </Scrollbar>

      {slots?.bottomArea}
    </Drawer>
  );
}
