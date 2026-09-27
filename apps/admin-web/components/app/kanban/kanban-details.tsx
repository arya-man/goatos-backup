'use client';

// Mesha kanban adapter (components/app, NOT a template file) built on the MUI Minimal template (sections/kanban/details/kanban-details.tsx drawer
// shell + BlockLabel, and sections/kanban/details/kanban-details-toolbar.tsx header row).
// Changes: presentational only -- the page passes the toolbar's status control, its trailing
// actions, the tab list and each tab's body (the template's mock task, like/delete handlers,
// date-range picker and contacts dialog are not copied). The close button shows at every width
// (the template shows it below `sm` only) and its accessible name is page copy.
import type { DrawerProps } from '@mui/material/Drawer';

import Box from '@mui/material/Box';
import Tab from '@mui/material/Tab';
import Tabs from '@mui/material/Tabs';
import Drawer from '@mui/material/Drawer';
import Tooltip from '@mui/material/Tooltip';
import IconButton from '@mui/material/IconButton';
import { styled } from '@mui/material/styles';

import { Iconify } from '@/components/minimal/iconify';
import { Scrollbar } from '@/components/minimal/scrollbar';

// ----------------------------------------------------------------------

export const BlockLabel = styled('span')(({ theme }) => ({
  ...theme.typography.caption,
  width: 100,
  flexShrink: 0,
  color: theme.vars.palette.text.secondary,
  fontWeight: theme.typography.fontWeightSemiBold,
}));

// ----------------------------------------------------------------------

export type KanbanDetailsTab = { value: string; label: React.ReactNode };

export type KanbanDetailsProps = Omit<DrawerProps, 'onClose' | 'children'> & {
  open: boolean;
  onClose: () => void;
  /** Left of the toolbar: the template's soft status Button (or any status reading). */
  status?: React.ReactNode;
  /** Right of the toolbar, before close. */
  actions?: React.ReactNode;
  closeLabel: string;
  tabs?: KanbanDetailsTab[];
  tab?: string;
  onChangeTab?: (value: string) => void;
  /** Rendered under the scrolling body (the template's comment input slot). */
  footer?: React.ReactNode;
  /** Accessible name of the drawer paper. */
  ariaLabel?: string;
  children: React.ReactNode;
};

export function KanbanDetails({
  open,
  onClose,
  status,
  actions,
  closeLabel,
  tabs,
  tab,
  onChangeTab,
  footer,
  ariaLabel,
  children,
  slotProps,
  ...other
}: KanbanDetailsProps) {
  const renderToolbar = () => (
    <Box
      sx={[
        (theme) => ({
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          p: theme.spacing(2.5, 1, 2.5, 2.5),
          borderBottom: `solid 1px ${theme.vars.palette.divider}`,
        }),
      ]}
    >
      {status}

      <Box component="span" sx={{ flexGrow: 1 }} />

      <Box sx={{ display: 'flex', alignItems: 'center' }}>
        {actions}
        <Tooltip title={closeLabel}>
          <IconButton onClick={onClose} aria-label={closeLabel}>
            <Iconify icon="mingcute:close-line" />
          </IconButton>
        </Tooltip>
      </Box>
    </Box>
  );

  const renderTabs = () =>
    tabs && tabs.length > 1 ? (
      <Tabs
        value={tab ?? tabs[0]?.value}
        onChange={(_event, value: string) => onChangeTab?.(value)}
        variant="fullWidth"
        indicatorColor="custom"
        sx={{ '--item-padding-x': 0 }}
      >
        {tabs.map((item) => (
          <Tab key={item.value} value={item.value} label={item.label} />
        ))}
      </Tabs>
    ) : null;

  return (
    <Drawer
      open={open}
      onClose={onClose}
      anchor="right"
      slotProps={{
        ...slotProps,
        paper: {
          ...(slotProps?.paper as object | undefined),
          'aria-label': ariaLabel,
          sx: { width: { xs: 1, sm: 480 }, maxWidth: '100vw', display: 'flex', flexDirection: 'column' },
        } as object,
      }}
      {...other}
    >
      {renderToolbar()}
      {renderTabs()}

      <Scrollbar fillContent sx={{ py: 3, px: 2.5 }}>
        {children}
      </Scrollbar>

      {footer}
    </Drawer>
  );
}
