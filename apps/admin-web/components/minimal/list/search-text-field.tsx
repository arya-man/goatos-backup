'use client';

// The template list toolbar's keyword search (sections/user/user-table-toolbar.tsx): an outlined
// TextField with the magnifier start adornment. Serializable props, so server pages can drop it
// into a GET form; controlled callers pass value/onChange.
import type { Theme, SxProps } from '@mui/material/styles';

import TextField from '@mui/material/TextField';
import InputAdornment from '@mui/material/InputAdornment';

import { Iconify } from '../iconify';

export type SearchTextFieldProps = {
  name?: string;
  value?: string;
  defaultValue?: string;
  placeholder?: string;
  ariaLabel?: string;
  disabled?: boolean;
  type?: 'search' | 'text';
  onChange?: (value: string) => void;
  onBlur?: () => void;
  onEnter?: () => void;
  className?: string;
  sx?: SxProps<Theme>;
};

export function SearchTextField({ name, value, defaultValue, placeholder, ariaLabel, disabled, type = 'search', onChange, onBlur, onEnter, className, sx }: SearchTextFieldProps) {
  return (
    <TextField
      fullWidth
      type={type}
      name={name}
      value={value}
      defaultValue={defaultValue}
      placeholder={placeholder}
      disabled={disabled}
      className={className}
      onChange={onChange ? (event) => onChange(event.target.value) : undefined}
      onBlur={onBlur}
      onKeyDown={
        onEnter
          ? (event) => {
              if (event.key === 'Enter') {
                event.preventDefault();
                onEnter();
              }
            }
          : undefined
      }
      sx={sx}
      slotProps={{
        htmlInput: { 'aria-label': ariaLabel ?? placeholder },
        input: {
          startAdornment: (
            <InputAdornment position="start">
              <Iconify icon="eva:search-fill" sx={{ color: 'text.disabled' }} />
            </InputAdornment>
          ),
        },
      }}
    />
  );
}
