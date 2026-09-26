'use client';

import type { Breakpoint } from '@mui/material/styles';
import type { AuthSplitSectionProps } from './section';
import type { AuthSplitContentProps } from './content';
import type { MainSectionProps, HeaderSectionProps, LayoutSectionProps } from '../core';

import { merge } from 'es-toolkit';

import Box from '@mui/material/Box';

import { Logo } from '@/layouts/template/logo';

import { AuthSplitSection } from './section';
import { AuthSplitContent } from './content';
import { MainSection, LayoutSection, HeaderSection } from '../core';

// Template pattern: Minimal_TypeScript_v7.7.0 next-ts src/layouts/auth-split/layout.tsx.
// Mesha adaptation: header carries only the Logo (no FAQs link, no template SettingsButton — the
// theme toggle already lives inside the auth card via GoogleLogin). No Alert top slot.

type LayoutBaseProps = Pick<LayoutSectionProps, 'sx' | 'children' | 'cssVars'>;

export type AuthSplitLayoutProps = LayoutBaseProps & {
  layoutQuery?: Breakpoint;
  logoText?: string;
  headerRight?: React.ReactNode;
  slotProps?: {
    header?: HeaderSectionProps;
    main?: MainSectionProps;
    section?: AuthSplitSectionProps;
    content?: AuthSplitContentProps;
  };
};

export function AuthSplitLayout({
  sx,
  cssVars,
  children,
  slotProps,
  logoText,
  headerRight,
  layoutQuery = 'md',
}: AuthSplitLayoutProps) {
  const renderHeader = () => {
    const headerSlotProps: HeaderSectionProps['slotProps'] = {
      container: { maxWidth: false },
    };

    const headerSlots: HeaderSectionProps['slots'] = {
      leftArea: <Logo text={logoText} />,
      rightArea: headerRight ? (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: { xs: 1, sm: 1.5 } }}>
          {headerRight}
        </Box>
      ) : null,
    };

    return (
      <HeaderSection
        disableElevation
        layoutQuery={layoutQuery}
        {...slotProps?.header}
        slots={{ ...headerSlots, ...slotProps?.header?.slots }}
        slotProps={merge(headerSlotProps, slotProps?.header?.slotProps ?? {})}
        sx={[
          { position: { [layoutQuery]: 'fixed' }, pt: 'env(safe-area-inset-top, 0px)' },
          ...(Array.isArray(slotProps?.header?.sx) ? slotProps.header.sx : [slotProps?.header?.sx]),
        ]}
      />
    );
  };

  const renderMain = () => (
    <MainSection
      {...slotProps?.main}
      sx={[
        (theme) => ({
          minHeight: '100dvh',
          [theme.breakpoints.up(layoutQuery)]: { flexDirection: 'row' },
        }),
        ...(Array.isArray(slotProps?.main?.sx) ? slotProps.main.sx : [slotProps?.main?.sx]),
      ]}
    >
      <AuthSplitSection layoutQuery={layoutQuery} {...slotProps?.section} />
      <AuthSplitContent layoutQuery={layoutQuery} {...slotProps?.content}>
        {children}
      </AuthSplitContent>
    </MainSection>
  );

  return (
    <LayoutSection
      headerSection={renderHeader()}
      footerSection={null}
      cssVars={{ '--layout-auth-content-width': '420px', ...cssVars }}
      sx={sx}
    >
      {renderMain()}
    </LayoutSection>
  );
}
