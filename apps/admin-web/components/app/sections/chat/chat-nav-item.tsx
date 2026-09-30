'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/chat/chat-nav-item.tsx. Anatomy guarded. The demo IChatConversation / participants /
// mocked user / router become props: `conversation` ({ id, unreadCount }), `displayName`,
// `displayText`, `lastActivity` (a preformatted caption), `avatar` (the Avatar's content) and
// `onClickConversation`; the group-avatar branch is not carried (one assistant per chat);
// declared: `aria-current` on the button.
import type { ReactNode } from 'react';
import type { BadgeProps } from '@mui/material/Badge';

import { useCallback } from 'react';

import Box from '@mui/material/Box';
import Badge from '@mui/material/Badge';
import Avatar from '@mui/material/Avatar';
import Typography from '@mui/material/Typography';
import ListItemText from '@mui/material/ListItemText';
import useMediaQuery from '@mui/material/useMediaQuery';
import ListItemButton from '@mui/material/ListItemButton';

// ----------------------------------------------------------------------

type Props = {
  selected: boolean;
  collapse: boolean;
  onCloseMobile: () => void;
  conversation: { id: string; unreadCount: number };
  displayName: string;
  displayText?: ReactNode;
  lastActivity?: string;
  avatar?: ReactNode;
  avatarUrl?: string;
  status?: BadgeProps['variant'];
  onClickConversation: (id: string) => void;
};

export function ChatNavItem({
  selected,
  collapse,
  conversation,
  onCloseMobile,
  displayName,
  displayText,
  lastActivity,
  avatar,
  avatarUrl,
  status = 'invisible',
  onClickConversation,
}: Props) {
  const mdUp = useMediaQuery((theme) => theme.breakpoints.up('md'));

  const handleClickConversation = useCallback(() => {
    if (!mdUp) {
      onCloseMobile();
    }

    onClickConversation(conversation.id);
  }, [conversation.id, mdUp, onClickConversation, onCloseMobile]);

  const renderSingle = () => (
    <Badge variant={status} badgeContent=" ">
      <Avatar alt={displayName} src={avatarUrl} sx={{ width: 48, height: 48 }}>
        {avatar}
      </Avatar>
    </Badge>
  );

  return (
    <Box component="li" sx={{ display: 'flex' }}>
      <ListItemButton
        onClick={handleClickConversation}
        aria-current={selected ? 'true' : undefined}
        sx={{
          py: 1.5,
          px: 2.5,
          gap: 2,
          ...(selected && { bgcolor: 'action.selected' }),
        }}
      >
        <Badge
          color="error"
          overlap="circular"
          badgeContent={collapse ? conversation.unreadCount : 0}
        >
          {renderSingle()}
        </Badge>

        {!collapse && (
          <>
            <ListItemText
              primary={displayName}
              secondary={displayText}
              slotProps={{
                primary: { noWrap: true },
                secondary: {
                  noWrap: true,
                  sx: {
                    ...(conversation.unreadCount && {
                      color: 'text.primary',
                      fontWeight: 'fontWeightSemiBold',
                    }),
                  },
                },
              }}
            />

            <Box
              sx={{
                display: 'flex',
                alignSelf: 'stretch',
                alignItems: 'flex-end',
                flexDirection: 'column',
              }}
            >
              <Typography
                noWrap
                variant="body2"
                component="span"
                sx={{ mb: 1.5, fontSize: 12, color: 'text.disabled' }}
              >
                {lastActivity}
              </Typography>

              {!!conversation.unreadCount && (
                <Box
                  component="span"
                  sx={{
                    width: 8,
                    height: 8,
                    borderRadius: '50%',
                    bgcolor: 'info.main',
                  }}
                />
              )}
            </Box>
          </>
        )}
      </ListItemButton>
    </Box>
  );
}
