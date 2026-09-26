'use client';

// Ported from the licensed MUI Minimal template (components/upload/default: UploadArea +
// PlaceholderContainer styles, and the single-file state). The template drives it with
// react-dropzone; here the dropzone is the native file input itself (visually hidden inside the
// area), so a form or a ref reads the chosen file exactly as it read the old bare input, and a
// dropped file is written onto that same input.
import { useRef, useState } from 'react';

import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { styled } from '@mui/material/styles';

import { Iconify } from '../iconify';

const UploadArea = styled('label')(({ theme }) => ({
  minHeight: 160,
  outline: 'none',
  display: 'flex',
  cursor: 'pointer',
  overflow: 'hidden',
  position: 'relative',
  alignItems: 'center',
  justifyContent: 'center',
  padding: theme.spacing(3),
  borderRadius: theme.shape.borderRadius,
  transition: theme.transitions.create(['opacity']),
  backgroundColor: `rgba(${theme.vars.palette.grey['500Channel']} / 0.08)`,
  border: `1px dashed rgba(${theme.vars.palette.grey['500Channel']} / 0.2)`,
  '&:hover, &.drag-active': { opacity: 0.72 },
  '&.disabled': { opacity: 0.48, pointerEvents: 'none' },
  '&:focus-within': { outline: `2px solid ${theme.vars.palette.primary.main}`, outlineOffset: 2 },
}));

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
      className={[dragActive ? 'drag-active' : '', disabled ? 'disabled' : ''].filter(Boolean).join(' ')}
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
      <input
        ref={setRefs}
        type="file"
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
