'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/chat/chat-message-list.tsx. Anatomy guarded. The demo messages.map(ChatMessageItem)
// becomes `children` (the page renders one ChatMessageItem per turn), the scroll ref is the caller's
// (`scrollRef`: the assistant follows a streaming answer only while the reader is at the bottom);
// not carried: the template image Lightbox (attachments open the caller's own viewer, which also
// shows PDFs and text files).
import type { ReactNode, RefObject } from 'react';

import Stack from '@mui/material/Stack';
import LinearProgress from '@mui/material/LinearProgress';

import { Scrollbar } from '@/components/minimal/scrollbar';

// ----------------------------------------------------------------------

type Props = {
  loading: boolean;
  children?: ReactNode;
  scrollRef: RefObject<HTMLDivElement | null>;
};

export function ChatMessageList({ children, scrollRef, loading }: Props) {
  if (loading) {
    return (
      <Stack sx={{ flex: '1 1 auto', position: 'relative' }}>
        <LinearProgress
          color="inherit"
          sx={{
            top: 0,
            left: 0,
            width: 1,
            height: 2,
            borderRadius: 0,
            position: 'absolute',
          }}
        />
      </Stack>
    );
  }

  return (
    <>
      <Scrollbar
        ref={scrollRef}
        sx={{
          px: 3,
          pt: 5,
          pb: 3,
          flex: '1 1 auto',
        }}
      >
        {children}
      </Scrollbar>
    </>
  );
}
