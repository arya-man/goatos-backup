'use client';

import type { Breakpoint } from '@mui/material/styles';
import type { AuthSplitSectionProps } from '@/layouts/auth-split/section';
import type { AuthSplitContentProps } from '@/layouts/auth-split/content';
import type { MainSectionProps, HeaderSectionProps, LayoutSectionProps } from '@/layouts/core';

import { merge } from 'es-toolkit';

import { Logo } from '@/layouts/template/logo';
import { AuthSplitSection } from '@/layouts/auth-split/section';
import { AuthSplitContent } from '@/layouts/auth-split/content';
import { MainSection, LayoutSection, HeaderSection } from '@/layouts/core';

// ----------------------------------------------------------------------

// Template-derived copy of Minimal v7.7.0 next-ts src/layouts/auth-split/layout.tsx (anatomy pinned in
// docs/design/template-derived.json). Not carried: the hidden demo Alert, the header "Need help?" FAQs
// link and SettingsButton (the theme toggle lives in the auth card), and the demo auth-provider
// method icons (Mesha has one sign-in method). Declared WebView overrides: the fixed header clears the
// notch, and the main section fills the dynamic viewport.

type LayoutBaseProps = Pick<LayoutSectionProps, 'sx' | 'children' | 'cssVars'>;

export type AuthSplitLayoutProps = LayoutBaseProps & {
  layoutQuery?: Breakpoint;
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
  layoutQuery = 'md',
}: AuthSplitLayoutProps) {
  const renderHeader = () => {
    const headerSlotProps: HeaderSectionProps['slotProps'] = {
      container: { maxWidth: false },
    };

    const headerSlots: HeaderSectionProps['slots'] = {
      leftArea: (
        <>
          {/** @slot Logo */}
          <Logo />
        </>
      ),
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

  const renderFooter = () => null;

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
      <AuthSplitSection
        layoutQuery={layoutQuery}
        {...slotProps?.section}
      />
      <AuthSplitContent layoutQuery={layoutQuery} {...slotProps?.content}>
        {children}
      </AuthSplitContent>
    </MainSection>
  );

  return (
    <LayoutSection
      /** **************************************
       * @Header
       *************************************** */
      headerSection={renderHeader()}
      /** **************************************
       * @Footer
       *************************************** */
      footerSection={renderFooter()}
      /** **************************************
       * @Styles
       *************************************** */
      cssVars={{ '--layout-auth-content-width': '420px', ...cssVars }}
      sx={sx}
    >
      {renderMain()}
    </LayoutSection>
  );
}
