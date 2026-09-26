// Copied from the licensed MUI Minimal template (sections/user/user-table-toolbar.tsx),
// generalised: a controlled multi-select filter + keyword search + a "more" actions popover.
import type { SelectChangeEvent } from '@mui/material/Select';

import { useId } from 'react';
import { usePopover } from 'minimal-shared/hooks';

import Box from '@mui/material/Box';
import Select from '@mui/material/Select';
import MenuList from '@mui/material/MenuList';
import MenuItem from '@mui/material/MenuItem';
import Checkbox from '@mui/material/Checkbox';
import TextField from '@mui/material/TextField';
import InputLabel from '@mui/material/InputLabel';
import IconButton from '@mui/material/IconButton';
import FormControl from '@mui/material/FormControl';
import InputAdornment from '@mui/material/InputAdornment';

import { Iconify } from '../iconify';
import { phoneTapSx } from '../_shared/tap';
import { CustomPopover } from '../custom-popover';
import type { IconifyName } from '../iconify';

export type ListToolbarAction = { label: string; icon?: IconifyName; onClick: () => void };

export type ListToolbarProps = {
  search: string;
  onSearch: (value: string) => void;
  searchPlaceholder?: string;
  /** Optional multi-select filter (e.g. Role / Status / Farm). */
  select?: {
    label: string;
    options: { value: string; label: string }[];
    value: string[];
    onChange: (value: string[]) => void;
  };
  /** Items for the trailing "more" menu; hidden when empty. */
  actions?: ListToolbarAction[];
  /** Extra controls rendered before the search box (e.g. a date range button). */
  children?: React.ReactNode;
};

export function ListToolbar({ search, onSearch, searchPlaceholder = 'Search...', select, actions = [], children }: ListToolbarProps) {
  const menuActions = usePopover();
  const id = useId();

  const handleSelect = (event: SelectChangeEvent<string[]>) => {
    const next = typeof event.target.value === 'string' ? event.target.value.split(',') : event.target.value;
    select?.onChange(next);
  };

  return (
    <>
      <Box
        sx={{
          p: 2.5,
          gap: 2,
          display: 'flex',
          pr: { xs: 2.5, md: 1 },
          flexDirection: { xs: 'column', md: 'row' },
          alignItems: { xs: 'flex-end', md: 'center' },
        }}
      >
        {select ? (
          <FormControl sx={{ flexShrink: 0, width: { xs: 1, md: 200 } }}>
            <InputLabel htmlFor={`${id}-select`}>{select.label}</InputLabel>
            <Select
              multiple
              label={select.label}
              value={select.value}
              onChange={handleSelect}
              renderValue={(selected) =>
                selected.map((v) => select.options.find((o) => o.value === v)?.label ?? v).join(', ')
              }
              inputProps={{ id: `${id}-select` }}
              MenuProps={{ slotProps: { paper: { sx: { maxHeight: 240 } } } }}
            >
              {select.options.map((option) => (
                <MenuItem key={option.value} value={option.value}>
                  <Checkbox disableRipple size="small" checked={select.value.includes(option.value)} />
                  {option.label}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        ) : null}

        {children}

        <Box sx={{ gap: 2, width: 1, flexGrow: 1, display: 'flex', alignItems: 'center' }}>
          <TextField
            fullWidth
            value={search}
            onChange={(event) => onSearch(event.target.value)}
            placeholder={searchPlaceholder}
            slotProps={{
              htmlInput: { 'aria-label': searchPlaceholder },
              input: {
                startAdornment: (
                  <InputAdornment position="start">
                    <Iconify icon="eva:search-fill" sx={{ color: 'text.disabled' }} />
                  </InputAdornment>
                ),
              },
            }}
          />

          {actions.length ? (
            <IconButton aria-label="More actions" onClick={menuActions.onOpen} sx={phoneTapSx}>
              <Iconify icon="eva:more-vertical-fill" />
            </IconButton>
          ) : null}
        </Box>
      </Box>

      <CustomPopover
        open={menuActions.open}
        anchorEl={menuActions.anchorEl}
        onClose={menuActions.onClose}
        slotProps={{ arrow: { placement: 'right-top' } }}
      >
        <MenuList>
          {actions.map((action) => (
            <MenuItem
              key={action.label}
              onClick={() => {
                menuActions.onClose();
                action.onClick();
              }}
            >
              {action.icon ? <Iconify icon={action.icon} /> : null}
              {action.label}
            </MenuItem>
          ))}
        </MenuList>
      </CustomPopover>
    </>
  );
}
