'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/chat/chat-message-item.tsx. Anatomy guarded. The demo IChatMessage / participants /
// mocked user become props: `me` (the reader's own turn), `info` (the caption line: sender name),
// `avatar` (the Avatar content), `children` (the body: text, attachments, answer chart, sources),
// `actions` (in place of the reply / emoji / delete IconButtons: the answer's Copy). Not carried:
// the image-message branch (attachments render inside the body). Declared overrides: an answer's
// column and body fill the thread instead of the 320 bubble (`slotProps.column`, `slotProps.body`),
// so its tables and charts get a width; the hover actions stay visible on touch screens (no hover
// there: `slotProps.actions`).
import type { ReactNode } from 'react';
import type { SxProps, Theme } from '@mui/material/styles';

import Box from '@mui/material/Box';
import Stack from '@mui/material/Stack';
import Avatar from '@mui/material/Avatar';
import Typography from '@mui/material/Typography';

import { mergeSx } from '@/components/app/merge-sx';

// ----------------------------------------------------------------------

type Props = {
  me: boolean;
  info?: ReactNode;
  avatar?: ReactNode;
  avatarUrl?: string;
  firstName?: string;
  children?: ReactNode;
  actions?: ReactNode;
  slotProps?: { column?: SxProps<Theme>; body?: SxProps<Theme>; actions?: SxProps<Theme> };
};

export function ChatMessageItem({ me, info, avatar, avatarUrl, firstName, children, actions, slotProps }: Props) {
  const renderInfo = () => (
    <Typography
      noWrap
      variant="caption"
      sx={{ mb: 1, color: 'text.disabled', ...(!me && { mr: 'auto' }) }}
    >
      {info}
    </Typography>
  );

  const renderBody = () => (
    <Stack
      sx={mergeSx({
        p: 1.5,
        minWidth: 48,
        maxWidth: 320,
        borderRadius: 1,
        typography: 'body2',
        bgcolor: 'background.neutral',
        ...(me && { color: 'grey.800', bgcolor: 'primary.lighter' }),
      }, slotProps?.body)}
    >
      {children}
    </Stack>
  );

  const renderActions = () => (
    <Box
      className="message-actions"
      sx={mergeSx((theme) => ({
        pt: 0.5,
        left: 0,
        opacity: 0,
        top: '100%',
        display: 'flex',
        position: 'absolute',
        transition: theme.transitions.create(['opacity'], {
          duration: theme.transitions.duration.shorter,
        }),
        ...(me && { right: 0, left: 'unset' }),
      }), slotProps?.actions)}
    >
      {actions}
    </Box>
  );

  return (
    <Box sx={{ mb: 5, display: 'flex', justifyContent: me ? 'flex-end' : 'unset' }}>
      {!me && <Avatar alt={firstName} src={avatarUrl} sx={{ width: 32, height: 32, mr: 2 }}>{avatar}</Avatar>}

      <Stack sx={mergeSx({ alignItems: me ? 'flex-end' : 'flex-start' }, slotProps?.column)}>
        {renderInfo()}

        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            position: 'relative',
            '&:hover': { '& .message-actions': { opacity: 1 } },
          }}
        >
          {renderBody()}
          {renderActions()}
        </Box>
      </Stack>
    </Box>
  );
}
