'use client';

import type { Breakpoint } from '@mui/material/styles';
import type { NavSectionProps } from '@/layouts/template/nav-section';
import type { MainSectionProps, HeaderSectionProps, LayoutSectionProps } from '../core';

import { merge } from 'es-toolkit';
import { useBoolean } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import { useTheme } from '@mui/material/styles';

import { Logo } from '@/layouts/template/logo';
import { Iconify } from '@/layouts/template/iconify';
import { useSettingsContext } from '@/layouts/template/settings';

import { NavMobile } from './nav-mobile';
import { NavVertical } from './nav-vertical';
import { MenuButton } from '../components/menu-button';
import { dashboardLayoutVars, dashboardNavColorVars } from './css-vars';
import { MainSection, layoutClasses, HeaderSection, LayoutSection } from '../core';

// ----------------------------------------------------------------------

// Minimal v7.7.0 next-ts DashboardLayout, adapted for admin-web:
// - nav data, logo text, header right area and nav footer come from the shell (backend bootstrap contract)
//   instead of the template's mocks (_notifications, _contacts, _workspaces, useMockedUser);
// - the horizontal nav layout, searchbar, language/contacts/settings popovers are not carried;
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
  closeLabel?: string;
  headerRight?: React.ReactNode;
  navBottom?: React.ReactNode;
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
  closeLabel,
  headerRight,
  navBottom,
  layoutQuery = 'lg',
}: DashboardLayoutProps) {
  const theme = useTheme();

  const settings = useSettingsContext();

  const navVars = dashboardNavColorVars(theme, settings.state.navColor, settings.state.navLayout);

  const { value: open, onFalse: onClose, onTrue: onOpen } = useBoolean();

  const isNavMini = settings.state.navLayout === 'mini';

  const renderLogo = (sx?: object) => <Logo text={logoText} href={logoHref} sx={sx} />;

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
              // Mesha rule: the phone menu keeps a visible close control (Escape and a backdrop tap
              // are not discoverable once the menu covers the whole page). Template drawer close pattern.
              topArea: (
                <Box sx={{ pl: 3.5, pr: 1.5, pt: 2.5, pb: 1, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  {renderLogo()}
                  <IconButton onClick={onClose} aria-label={closeLabel} data-nav-close>
                    <Iconify icon="mingcute:close-line" />
                  </IconButton>
                </Box>
              ),
              bottomArea: navBottom,
            }}
          />
        </>
      ) : (
        renderLogo()
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
        // Mesha WebView: header clears the status-bar notch under viewport-fit=cover.
        sx={[
          { pt: 'env(safe-area-inset-top, 0px)' },
          ...(Array.isArray(slotProps?.header?.sx) ? slotProps.header.sx : [slotProps?.header?.sx]),
        ]}
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
        bottomArea: isNavMini ? null : navBottom,
      }}
      onToggleNav={() => settings.setField('navLayout', isNavMini ? 'vertical' : 'mini')}
    />
  );

  const renderMain = () => <MainSection {...slotProps?.main}>{children}</MainSection>;

  return (
    <LayoutSection
      headerSection={renderHeader()}
      sidebarSection={showNav ? renderSidebar() : null}
      footerSection={null}
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
