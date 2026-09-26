// Copied from the licensed MUI Minimal template (sections/calendar/calendar-filters.tsx drawer shell):
// right-anchored Drawer with a header (title, optional reset-with-dot, close), a Scrollbar body
// and an optional sticky footer. Used for both filter drawers and detail drawers.
import type { DrawerProps } from '@mui/material/Drawer';

import Box from '@mui/material/Box';
import Badge from '@mui/material/Badge';
import Drawer from '@mui/material/Drawer';
import Divider from '@mui/material/Divider';
import Tooltip from '@mui/material/Tooltip';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';

import { Iconify } from '../iconify';
import { Scrollbar } from '../scrollbar';
import { phoneTapSx } from '../_shared/tap';

export type MinimalDrawerProps = Omit<DrawerProps, 'title' | 'onClose'> & {
  open: boolean;
  onClose: () => void;
  title: React.ReactNode;
  /** Shows the reset button; `canReset` lights its dot. */
  onReset?: () => void;
  canReset?: boolean;
  footer?: React.ReactNode;
  /** Paper width from sm up; phones get the full width. Default 320. */
  width?: number;
  /** Filters keep the page visible (invisible backdrop); details dim it. */
  invisibleBackdrop?: boolean;
  /** Mesha: accessible name of the close button (page copy; the template says "Close"). */
  closeLabel?: string;
};

export function MinimalDrawer({
  open,
  onClose,
  title,
  onReset,
  canReset,
  footer,
  width = 320,
  invisibleBackdrop = false,
  closeLabel = 'Close',
  children,
  slotProps,
  ...other
}: MinimalDrawerProps) {
  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      slotProps={{
        ...slotProps,
        backdrop: { invisible: invisibleBackdrop },
        paper: { sx: { width: { xs: 1, sm: width }, maxWidth: '100vw', display: 'flex', flexDirection: 'column' } },
      }}
      {...other}
    >
      <Box sx={{ py: 2, pr: 1, pl: 2.5, display: 'flex', alignItems: 'center', gap: 0.5 }}>
        <Typography variant="h6" sx={{ flexGrow: 1, minWidth: 0 }} noWrap>
          {title}
        </Typography>

        {onReset ? (
          <Tooltip title="Reset">
            <IconButton aria-label="Reset" onClick={onReset} sx={phoneTapSx}>
              <Badge color="error" variant="dot" invisible={!canReset}>
                <Iconify icon="solar:restart-bold" />
              </Badge>
            </IconButton>
          </Tooltip>
        ) : null}

        <IconButton aria-label={closeLabel} onClick={onClose} sx={phoneTapSx}>
          <Iconify icon="mingcute:close-line" />
        </IconButton>
      </Box>

      <Divider sx={{ borderStyle: 'dashed' }} />

      <Scrollbar sx={{ flex: '1 1 auto', minHeight: 0 }}>{children}</Scrollbar>

      {footer ? (
        <>
          <Divider sx={{ borderStyle: 'dashed' }} />
          <Box sx={{ p: 2.5, gap: 1.5, display: 'flex', justifyContent: 'flex-end' }}>{footer}</Box>
        </>
      ) : null}
    </Drawer>
  );
}

/** A labelled block inside a filters drawer (template calendar-filters section pattern). */
export function DrawerSection({ title, children }: { title: React.ReactNode; children: React.ReactNode }) {
  return (
    <Box sx={{ px: 2.5, py: 3, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      <Typography variant="subtitle2">{title}</Typography>
      {children}
    </Box>
  );
}
