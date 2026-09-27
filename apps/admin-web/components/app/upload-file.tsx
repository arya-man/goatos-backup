'use client';

// File picker adapter (components/app): the VERBATIM template upload area
// (components/minimal/upload/default/styles.tsx UploadArea, template state classes) rendered as a
// <label> around the native file input (visually hidden inside), so a form or a ref reads the chosen
// file exactly as it read the old bare input, and a dropped file is written onto that same input.
// The template drives the area with react-dropzone; the native input keeps form behaviour unchanged.
import { useRef, useState } from 'react';

import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import { Iconify } from '@/components/minimal/iconify';
import { uploadClasses } from '@/components/minimal/upload/classes';
import { UploadArea } from '@/components/minimal/upload/default/styles';

const HIDDEN_INPUT: React.CSSProperties = {
  border: 0,
  margin: -1,
  padding: 0,
  width: 1,
  height: 1,
  overflow: 'hidden',
  position: 'absolute',
  whiteSpace: 'nowrap',
  clip: 'rect(0 0 0 0)',
};

export type UploadFileProps = {
  accept?: string;
  disabled?: boolean;
  name?: string;
  inputRef?: React.Ref<HTMLInputElement>;
  /** Template placeholder title ("Drop or select a file"), from page copy. */
  title: React.ReactNode;
  /** Template placeholder description, from page copy. */
  description?: React.ReactNode;
  ariaLabel?: string;
  onFileChange?: (file: File | null) => void;
  testId?: string;
};

export function UploadFile({ accept, disabled, name, inputRef, title, description, ariaLabel, onFileChange, testId }: UploadFileProps) {
  const [dragActive, setDragActive] = useState(false);
  const [fileName, setFileName] = useState('');
  const own = useRef<HTMLInputElement | null>(null);

  const setRefs = (node: HTMLInputElement | null) => {
    own.current = node;
    if (typeof inputRef === 'function') inputRef(node);
    else if (inputRef && typeof inputRef === 'object') (inputRef as React.MutableRefObject<HTMLInputElement | null>).current = node;
  };

  const pick = (files: FileList | null) => {
    const file = files?.[0] ?? null;
    setFileName(file?.name ?? '');
    onFileChange?.(file);
  };

  return (
    <UploadArea
      as="label"
      className={[dragActive ? uploadClasses.state.dragActive : '', disabled ? uploadClasses.state.disabled : ''].filter(Boolean).join(' ')}
      // Drawer-sized (template default is the 280px page dropzone); keyboard focus shows on the area.
      sx={{ minHeight: 'calc(var(--sp-2) * 10)', '&:focus-within': { outline: 2, outlineStyle: 'solid', outlineColor: 'primary.main', outlineOffset: 2 } }}
      onDragOver={(event) => {
        event.preventDefault();
        setDragActive(true);
      }}
      onDragLeave={() => setDragActive(false)}
      onDrop={(event) => {
        event.preventDefault();
        setDragActive(false);
        if (disabled || !own.current || !event.dataTransfer.files.length) return;
        own.current.files = event.dataTransfer.files;
        pick(event.dataTransfer.files);
      }}
    >
      <input type="file"
        ref={setRefs}
        name={name}
        accept={accept}
        disabled={disabled}
        aria-label={ariaLabel}
        style={HIDDEN_INPUT}
        onChange={(event) => pick(event.currentTarget.files)}
        data-testid={testId}
      />
      <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 1, textAlign: 'center', minWidth: 0 }}>
        <Iconify icon="eva:cloud-upload-fill" width={40} sx={{ color: 'text.disabled' }} />
        <Typography variant="h6" component="span" sx={{ overflowWrap: 'anywhere' }}>
          {fileName || title}
        </Typography>
        {description && !fileName ? (
          <Typography variant="body2" component="span" sx={{ color: 'text.secondary' }}>
            {description}
          </Typography>
        ) : null}
      </Box>
    </UploadArea>
  );
}
