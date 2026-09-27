'use client';

import type { Breakpoint } from '@mui/material/styles';
import type { NavSectionProps } from '@/layouts/template/nav-section';
import type { MainSectionProps, HeaderSectionProps, LayoutSectionProps } from '@/layouts/core';

import { merge } from 'es-toolkit';
import { useBoolean } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import { useTheme } from '@mui/material/styles';

import { mergeSx } from '@/components/app/merge-sx';
import { Logo } from '@/layouts/template/logo';
import { useSettingsContext } from '@/layouts/template/settings';

import { NavMobile } from './nav-mobile';
import { NavVertical } from './nav-vertical';
import { MenuButton } from '@/layouts/components/menu-button';
import { dashboardLayoutVars, dashboardNavColorVars } from '@/layouts/dashboard/css-vars';
import { MainSection, layoutClasses, HeaderSection, LayoutSection } from '@/layouts/core';

// ----------------------------------------------------------------------

// Minimal v7.7.0 next-ts DashboardLayout, adapted for admin-web:
// - nav data, logo text and header right area come from the shell (backend bootstrap contract)
//   instead of the template's mocks (_notifications, _contacts, _workspaces, useMockedUser);
// - the horizontal nav layout, searchbar, language/contacts/settings popovers and the NavUpgrade card are not carried;
// - `showNav=false` (contract nav_chrome "minimal") drops the sidebar and shows the logo in the header.
// Structure, breakpoints (layoutQuery lg), css vars, header/nav/main sections are template-exact.

type LayoutBaseProps = Pick<LayoutSectionProps, 'sx' | 'children' | 'cssVars'>;

export type DashboardLayoutProps = LayoutBaseProps & {
  layoutQuery?: Breakpoint;
  navData: NavSectionProps['data'];
  showNav?: boolean;
  logoText?: string;
  logoHref?: string;
  navLabel?: string;
  menuLabel?: string;
  headerLeft?: React.ReactNode;
  headerRight?: React.ReactNode;
  slotProps?: {
    header?: HeaderSectionProps;
    main?: MainSectionProps;
  };
};

export function DashboardLayout({
  sx,
  cssVars,
  children,
  slotProps,
  navData,
  showNav = true,
  logoText,
  logoHref = '/',
  navLabel,
  menuLabel,
  headerLeft,
  headerRight,
  layoutQuery = 'lg',
}: DashboardLayoutProps) {
  const theme = useTheme();

  const settings = useSettingsContext();

  const navVars = dashboardNavColorVars(theme, settings.state.navColor, settings.state.navLayout);

  const { value: open, onFalse: onClose, onTrue: onOpen } = useBoolean();

  const isNavMini = settings.state.navLayout === 'mini';

  const renderLogo = () => <Logo text={logoText} href={logoHref} />;

  const renderHeader = () => {
    const headerSlotProps: HeaderSectionProps['slotProps'] = {
      container: {
        maxWidth: false,
        sx: { px: { [layoutQuery]: 5 } },
      },
    };

    const headerSlots: HeaderSectionProps['slots'] = {
      leftArea: showNav ? (
        <>
          {/** @slot Nav mobile */}
          <MenuButton
            onClick={onOpen}
            aria-label={menuLabel}
            aria-expanded={open}
            data-nav-open
            sx={{ mr: 1, ml: -1, [theme.breakpoints.up(layoutQuery)]: { display: 'none' } }}
          />
          <NavMobile
            data={navData}
            open={open}
            onClose={onClose}
            cssVars={navVars.section}
            className="msh-side"
            aria-label={navLabel}
            slots={{
              topArea: (
                <Box sx={{ pl: 3.5, pt: 2.5, pb: 1 }}>{renderLogo()}</Box>
              ),
            }}
          />
          {/** @slot Workspace popover (Mesha: park scope switcher) */}
          {headerLeft}
        </>
      ) : (
        <>
          {renderLogo()}
          {headerLeft}
        </>
      ),
      rightArea: (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: { xs: 0, sm: 0.75 } }}>{headerRight}</Box>
      ),
    };

    return (
      <HeaderSection
        layoutQuery={layoutQuery}
        disableElevation
        {...slotProps?.header}
        slots={{ ...headerSlots, ...slotProps?.header?.slots }}
        slotProps={merge(headerSlotProps, slotProps?.header?.slotProps ?? {})}
        // Declared override (Mesha WebView): the header clears the status-bar notch under viewport-fit=cover.
        sx={mergeSx({ pt: 'env(safe-area-inset-top, 0px)' }, slotProps?.header?.sx)}
      />
    );
  };

  const renderSidebar = () => (
    <NavVertical
      data={navData}
      isNavMini={isNavMini}
      layoutQuery={layoutQuery}
      cssVars={navVars.section}
      className="msh-side"
      aria-label={navLabel}
      slots={{
        topArea: isNavMini ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 2.5 }}>{renderLogo()}</Box>
        ) : (
          <Box sx={{ pl: 3.5, pt: 2.5, pb: 1 }}>{renderLogo()}</Box>
        ),
      }}
      onToggleNav={() =>
        settings.setField(
          'navLayout',
          settings.state.navLayout === 'vertical' ? 'mini' : 'vertical'
        )
      }
    />
  );

  const renderFooter = () => null;

  const renderMain = () => <MainSection {...slotProps?.main}>{children}</MainSection>;

  return (
    <LayoutSection
      headerSection={renderHeader()}
      sidebarSection={showNav ? renderSidebar() : null}
      footerSection={renderFooter()}
      cssVars={{ ...dashboardLayoutVars(theme), ...navVars.layout, ...cssVars }}
      sx={[
        {
          [`& .${layoutClasses.sidebarContainer}`]: {
            [theme.breakpoints.up(layoutQuery)]: {
              pl: showNav
                ? isNavMini
                  ? 'var(--layout-nav-mini-width)'
                  : 'var(--layout-nav-vertical-width)'
                : 0,
              transition: theme.transitions.create(['padding-left'], {
                easing: 'var(--layout-transition-easing)',
                duration: 'var(--layout-transition-duration)',
              }),
            },
          },
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      {renderMain()}
    </LayoutSection>
  );
}
