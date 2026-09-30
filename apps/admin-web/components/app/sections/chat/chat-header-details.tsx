'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/chat/chat-header-details.tsx. Anatomy guarded. The demo participants become the
// assistant identity props (`name`, `status` caption line, `avatar` content); the phone / video
// IconButtons -> the `actions` slot (the panel's window controls); the Hide notifications / Block /
// Report menu items -> the `menuActions` slot (Rename), Delete -> `onDelete`; the group-avatar
// branch and the loading skeleton are not carried. Declared: the sidebar toggle follows the chat
// nav's md breakpoint (template lg: that toggles the contact details rail, which has no Mesha
// equivalent), the more IconButton renders only when there is a chat to act on, aria-labels on
// the icon buttons.
import type { ReactNode } from 'react';
import type { BadgeProps } from '@mui/material/Badge';
import type { UseNavCollapseReturn } from '@/components/minimal/sections/chat/hooks/use-collapse-nav';

import { useCallback } from 'react';
import { usePopover } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import Badge from '@mui/material/Badge';
import Avatar from '@mui/material/Avatar';
import Divider from '@mui/material/Divider';
import MenuList from '@mui/material/MenuList';
import MenuItem from '@mui/material/MenuItem';
import IconButton from '@mui/material/IconButton';
import ListItemText from '@mui/material/ListItemText';
import useMediaQuery from '@mui/material/useMediaQuery';

import { Iconify } from '@/components/minimal/iconify';
import { CustomPopover } from '@/components/minimal/custom-popover';

// ----------------------------------------------------------------------

type Props = {
  name: ReactNode;
  status?: ReactNode;
  avatar?: ReactNode;
  avatarUrl?: string;
  badge?: BadgeProps['variant'];
  collapseNav: UseNavCollapseReturn;
  actions?: ReactNode;
  menuActions?: (close: () => void) => ReactNode;
  onDelete?: () => void;
  deleteLabel?: string;
  toggleLabel: string;
  moreLabel: string;
};

export function ChatHeaderDetails({
  name,
  status,
  avatar,
  avatarUrl,
  badge = 'invisible',
  collapseNav,
  actions,
  menuActions: renderMenuItems,
  onDelete,
  deleteLabel = 'Delete',
  toggleLabel,
  moreLabel,
}: Props) {
  const mdUp = useMediaQuery((theme) => theme.breakpoints.up('md'));

  const menuActions = usePopover();

  const { collapseDesktop, onCollapseDesktop, onOpenMobile } = collapseNav;

  const handleToggleNav = useCallback(() => {
    if (mdUp) {
      onCollapseDesktop();
    } else {
      onOpenMobile();
    }
  }, [mdUp, onCollapseDesktop, onOpenMobile]);

  const renderSingle = () => (
    <Box sx={{ gap: 2, display: 'flex', alignItems: 'center' }}>
      <Badge variant={badge} badgeContent=" ">
        <Avatar src={avatarUrl} alt={typeof name === 'string' ? name : undefined}>
          {avatar}
        </Avatar>
      </Badge>

      <ListItemText
        primary={name}
        secondary={status}
      />
    </Box>
  );

  const renderMenuActions = () => (
    <CustomPopover
      open={menuActions.open}
      anchorEl={menuActions.anchorEl}
      onClose={menuActions.onClose}
    >
      <MenuList>
        {renderMenuItems?.(menuActions.onClose)}

        <Divider sx={{ borderStyle: 'dashed' }} />

        <MenuItem onClick={() => { menuActions.onClose(); onDelete?.(); }} sx={{ color: 'error.main' }}>
          <Iconify icon="solar:trash-bin-trash-bold" />
          {deleteLabel}
        </MenuItem>
      </MenuList>
    </CustomPopover>
  );

  return (
    <>
      {renderSingle()}

      <Box sx={{ flexGrow: 1, display: 'flex', justifyContent: 'flex-end' }}>
        {actions}

        <IconButton onClick={handleToggleNav} aria-label={toggleLabel}>
          <Iconify
            icon={!collapseDesktop ? 'custom:sidebar-unfold-fill' : 'custom:sidebar-fold-fill'}
          />
        </IconButton>

        {onDelete ? (
          <IconButton onClick={menuActions.onOpen} aria-label={moreLabel}>
            <Iconify icon="eva:more-vertical-fill" />
          </IconButton>
        ) : null}
      </Box>

      {renderMenuActions()}
    </>
  );
}
