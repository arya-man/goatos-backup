'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/chat/chat-nav.tsx. Anatomy guarded. The demo contacts / conversations store /
// router / mocked user become props: the conversation rows are `children` (ChatNavItem per chat),
// ChatNavAccount -> the `title` slot, the compose IconButton -> `onCompose` (a new chat) with its
// `composeLabel`; not carried: the contact search field and its results (the chats have no contact
// book) and the loading skeleton (the list keeps its last rows while it refreshes); declared:
// aria-labels on the toggle, compose and phone-toggle buttons, and the phone drawer keeps the
// visible template backdrop (template: invisible) so the chats open behind a scrim
// (docs/design/redesign-regression-guard.md, Ask Mesha phone layout).
import type { ReactNode } from 'react';
import type { UseNavCollapseReturn } from '@/components/minimal/sections/chat/hooks/use-collapse-nav';

import { useEffect, useCallback } from 'react';

import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';
import IconButton from '@mui/material/IconButton';
import useMediaQuery from '@mui/material/useMediaQuery';

import { Iconify } from '@/components/minimal/iconify';
import { Scrollbar } from '@/components/minimal/scrollbar';
import { ToggleButton } from '@/components/minimal/sections/chat/styles';

// ----------------------------------------------------------------------

const NAV_WIDTH = 320;
const NAV_COLLAPSE_WIDTH = 96;

type Props = {
  title?: ReactNode;
  children?: ReactNode;
  collapseNav: UseNavCollapseReturn;
  onCompose: () => void;
  composeLabel: string;
  toggleLabel: string;
};

export function ChatNav({ title, children, collapseNav, onCompose, composeLabel, toggleLabel }: Props) {
  const mdUp = useMediaQuery((theme) => theme.breakpoints.up('md'));

  const {
    openMobile,
    onOpenMobile,
    onCloseMobile,
    onCloseDesktop,
    collapseDesktop,
    onCollapseDesktop,
  } = collapseNav;

  useEffect(() => {
    if (!mdUp) {
      onCloseDesktop();
    }
  }, [onCloseDesktop, mdUp]);

  const handleToggleNav = useCallback(() => {
    if (mdUp) {
      onCollapseDesktop();
    } else {
      onCloseMobile();
    }
  }, [mdUp, onCloseMobile, onCollapseDesktop]);

  const handleClickCompose = useCallback(() => {
    if (!mdUp) {
      onCloseMobile();
    }
    onCompose();
  }, [mdUp, onCloseMobile, onCompose]);

  const renderList = () => (
    <nav>
      <Box component="ul">
        {children}
      </Box>
    </nav>
  );

  const renderContent = () => (
    <>
      <Box
        sx={{
          pt: 2.5,
          px: 2.5,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        {!collapseDesktop && (
          <>
            {title}
            <Box sx={{ flexGrow: 1 }} />
          </>
        )}

        <IconButton onClick={handleToggleNav} aria-label={toggleLabel}>
          <Iconify
            icon={collapseDesktop ? 'eva:arrow-ios-forward-fill' : 'eva:arrow-ios-back-fill'}
          />
        </IconButton>

        {!collapseDesktop && (
          <IconButton onClick={handleClickCompose} aria-label={composeLabel}>
            <Iconify width={24} icon="solar:chat-round-dots-bold" />
          </IconButton>
        )}
      </Box>

      <Scrollbar sx={{ pb: 1 }}>
        {renderList()}
      </Scrollbar>
    </>
  );

  return (
    <>
      <ToggleButton onClick={onOpenMobile} aria-label={toggleLabel} sx={{ display: { md: 'none' } }}>
        <Iconify width={16} icon="solar:users-group-rounded-bold" />
      </ToggleButton>

      <Box
        sx={[
          (theme) => ({
            minHeight: 0,
            flex: '1 1 auto',
            width: NAV_WIDTH,
            flexDirection: 'column',
            display: { xs: 'none', md: 'flex' },
            borderRight: `solid 1px ${theme.vars.palette.divider}`,
            transition: theme.transitions.create(['width'], {
              duration: theme.transitions.duration.shorter,
            }),
            ...(collapseDesktop && { width: NAV_COLLAPSE_WIDTH }),
          }),
        ]}
      >
        {renderContent()}
      </Box>

      <Drawer
        anchor="left"
        open={openMobile}
        onClose={onCloseMobile}
        slotProps={{
          paper: { sx: { width: NAV_WIDTH } },
        }}
      >
        {renderContent()}
      </Drawer>
    </>
  );
}
