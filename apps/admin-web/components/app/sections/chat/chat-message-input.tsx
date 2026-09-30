'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/chat/chat-message-input.tsx. Anatomy guarded. The demo send / create-conversation
// actions, router and mocked user become props: `value` / `onChange`, `onKeyDown` (Enter sends,
// Shift+Enter breaks the line, Escape stops a running answer), `onPaste` (pasted files attach),
// `onAttach(files)` from the file input, `placeholder`, `send` (the send / stop IconButton slot) and
// `voice` (the mic IconButton, only where the browser can dictate). Not carried: the emoji and
// gallery buttons (no emoji picker; one attach button takes images and files). Declared overrides:
// the field is multiline (up to 4 rows, so its height is a 56 minimum instead of a fixed 56),
// Enter is read on keydown (template keyup) so a sent line never leaves a newline behind, the file
// input is `hidden` (template inline style) and takes several readable types, aria-labels on the
// field and the attach button.
import type { ReactNode } from 'react';

import { useRef, useCallback } from 'react';

import Box from '@mui/material/Box';
import InputBase from '@mui/material/InputBase';
import IconButton from '@mui/material/IconButton';

import { Iconify } from '@/components/minimal/iconify';

// ----------------------------------------------------------------------

type Props = {
  value: string;
  disabled: boolean;
  placeholder: string;
  inputLabel: string;
  attachLabel: string;
  accept: string;
  onChange: (value: string) => void;
  onKeyDown: (event: React.KeyboardEvent<HTMLTextAreaElement | HTMLInputElement>) => void;
  onPaste: (event: React.ClipboardEvent<HTMLElement>) => void;
  onAttach: (files: FileList | null) => void;
  voice?: ReactNode;
  send: ReactNode;
};

export function ChatMessageInput({
  value,
  disabled,
  placeholder,
  inputLabel,
  attachLabel,
  accept,
  onChange,
  onKeyDown,
  onPaste,
  onAttach,
  voice,
  send,
}: Props) {
  const fileRef = useRef<HTMLInputElement>(null);

  const handleAttach = useCallback(() => {
    if (fileRef.current) {
      fileRef.current.click();
    }
  }, []);

  const handleChangeMessage = useCallback(
    (event: React.ChangeEvent<HTMLTextAreaElement | HTMLInputElement>) => {
      onChange(event.target.value);
    },
    [onChange]
  );

  return (
    <>
      <InputBase
        name="chat-message"
        id="chat-message-input"
        value={value}
        onKeyDown={onKeyDown}
        onChange={handleChangeMessage}
        placeholder={placeholder}
        disabled={disabled}
        multiline
        maxRows={4}
        onPaste={onPaste}
        slotProps={{ input: { 'aria-label': inputLabel } }}
        endAdornment={
          <Box sx={{ flexShrink: 0, display: 'flex' }}>
            <IconButton onClick={handleAttach} aria-label={attachLabel}>
              <Iconify icon="eva:attach-2-fill" />
            </IconButton>
            {voice}
            {send}
          </Box>
        }
        sx={[
          (theme) => ({
            px: 1,
            minHeight: 56,
            flexShrink: 0,
            borderTop: `solid 1px ${theme.vars.palette.divider}`,
          }),
        ]}
      />

      <input
        type="file"
        ref={fileRef}
        hidden
        multiple
        accept={accept}
        onChange={(event) => {
          onAttach(event.target.files);
          event.target.value = '';
        }}
      />
    </>
  );
}
