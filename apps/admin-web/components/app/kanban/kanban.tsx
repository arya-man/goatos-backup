"use client";

// Mesha kanban adapter (components/app, NOT a template file) built on the MUI Minimal template (sections/kanban: view vars, column/styles.tsx,
// column/kanban-column-toolbar.tsx, item/styles.tsx). Presentational only: no drag-and-drop,
// rename or add-task behaviour (pages own behaviour).
import type { BoxProps } from '@mui/material/Box';

import { varAlpha } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Avatar from '@mui/material/Avatar';
import { styled } from '@mui/material/styles';
import Typography from '@mui/material/Typography';
import AvatarGroup, { avatarGroupClasses } from '@mui/material/AvatarGroup';

import { Label } from '@/components/minimal/label';
import { Iconify } from '@/components/minimal/iconify';
import type { IconifyName } from '@/components/minimal/iconify';

export type KanbanPriority = 'low' | 'medium' | 'high';

export function KanbanBoard({ sx, children, ...other }: BoxProps) {
  return (
    <Box
      sx={[
        {
          '--kanban-item-gap': '16px',
          '--kanban-item-radius': '12px',
          '--kanban-column-gap': '24px',
          '--kanban-column-width': '336px',
          '--kanban-column-radius': '16px',
          '--kanban-column-pt': '20px',
          '--kanban-column-pb': '16px',
          '--kanban-column-px': '16px',
          pb: 2,
          gap: 'var(--kanban-column-gap)',
          display: 'flex',
          alignItems: 'flex-start',
          overflowX: 'auto',
          maxWidth: 1,
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      {children}
    </Box>
  );
}

const ColumnRoot = styled('section')(({ theme }) => ({
  flexShrink: 0,
  display: 'flex',
  flexDirection: 'column',
  gap: 'var(--kanban-item-gap)',
  width: 'min(var(--kanban-column-width), calc(100vw - var(--sp-3) * 2))',
  borderRadius: 'var(--kanban-column-radius)',
  backgroundColor: theme.vars.palette.background.neutral,
}));

const ColumnList = styled('ul')({
  margin: 0,
  minHeight: 'calc(var(--sp-2) * 5)',
  display: 'flex',
  listStyle: 'none',
  flexDirection: 'column',
  gap: 'var(--kanban-item-gap)',
  paddingLeft: 'var(--kanban-column-px)',
  paddingRight: 'var(--kanban-column-px)',
  paddingBottom: 'var(--kanban-column-pb)',
});

export type KanbanColumnProps = {
  title: React.ReactNode;
  count: number;
  /** Trailing header controls (e.g. a RowMenu/IconButton). */
  actions?: React.ReactNode;
  children?: React.ReactNode;
};

export function KanbanColumn({ title, count, actions, children }: KanbanColumnProps) {
  return (
    <ColumnRoot>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, pt: 'var(--kanban-column-pt)', px: 'var(--kanban-column-px)' }}>
        <Label sx={(theme) => ({ borderRadius: '50%', borderColor: varAlpha(theme.vars.palette.grey['500Channel'], 0.24) })}>{count}</Label>
        <Typography variant="h6" noWrap sx={{ flexGrow: 1, minWidth: 0, fontSize: 'var(--fs-subtitle1)' }}>
          {title}
        </Typography>
        {actions}
      </Box>
      <ColumnList>{children}</ColumnList>
    </ColumnRoot>
  );
}

/** Template item/styles.tsx `ItemRoot`, exported so a page can put its own card content in the template item shell. */
export const KanbanItemRoot = styled('li')(({ theme }) => ({
  flexShrink: 0,
  display: 'flex',
  position: 'relative',
  flexDirection: 'column',
  borderRadius: 'var(--kanban-item-radius)',
  backgroundColor: theme.vars.palette.common.white,
  transition: theme.transitions.create(['box-shadow', 'background-color']),
  ...theme.applyStyles('dark', { backgroundColor: theme.vars.palette.grey[900] }),
  '&:hover': { boxShadow: theme.vars.customShadows.z8 },
  '&[data-selected="true"]': { backgroundColor: theme.vars.palette.action.selected },
}));

const PRIORITY_ICON: Record<KanbanPriority, IconifyName> = {
  low: 'solar:double-alt-arrow-down-bold-duotone',
  medium: 'solar:double-alt-arrow-right-bold-duotone',
  high: 'solar:double-alt-arrow-up-bold-duotone',
};

export type KanbanTaskCardProps = {
  name: React.ReactNode;
  priority?: KanbanPriority;
  /** Secondary line (due date, farm, …) — already formatted by the page. */
  meta?: React.ReactNode;
  comments?: number;
  attachments?: number;
  assignees?: { id: string; name: string; avatarUrl?: string }[];
  selected?: boolean;
  onClick?: () => void;
};

export function KanbanTaskCard({ name, priority, meta, comments = 0, attachments = 0, assignees = [], selected, onClick }: KanbanTaskCardProps) {
  const info = (icon: IconifyName, count: number) => (
    <Box sx={{ gap: 0.25, display: 'flex', alignItems: 'center', typography: 'caption', color: 'text.disabled' }}>
      <Iconify width={16} icon={icon} />
      <Box component="span">{count}</Box>
    </Box>
  );
  const hasFooter = comments > 0 || attachments > 0 || assignees.length > 0;

  return (
    <KanbanItemRoot data-selected={selected ? 'true' : undefined}>
      <Box
        component={onClick ? 'button' : 'div'}
        type={onClick ? 'button' : undefined}
        onClick={onClick}
        sx={{
          position: 'relative',
          px: 2,
          py: 2.5,
          minHeight: 'var(--tap-min)',
          textAlign: 'left',
          border: 0,
          font: 'inherit',
          color: 'inherit',
          background: 'none',
          borderRadius: 'inherit',
          cursor: onClick ? 'pointer' : 'default',
        }}
      >
        {priority ? (
          <Iconify
            icon={PRIORITY_ICON[priority]}
            aria-label={`${priority} priority`}
            sx={{
              top: 4,
              right: 4,
              position: 'absolute',
              color: priority === 'low' ? 'info.main' : priority === 'medium' ? 'warning.main' : 'error.main',
            }}
          />
        ) : null}
        <Typography component="span" variant="subtitle2" sx={{ display: 'block', pr: 2.5 }}>
          {name}
        </Typography>
        {meta ? (
          <Typography component="span" variant="caption" sx={{ display: 'block', mt: 0.5, color: 'text.secondary' }}>
            {meta}
          </Typography>
        ) : null}
        {hasFooter ? (
          <Box sx={{ mt: 2, display: 'flex', alignItems: 'center' }}>
            <Box sx={{ gap: 1, display: 'flex', alignItems: 'center' }}>
              {comments > 0 && info('solar:chat-round-dots-bold', comments)}
              {attachments > 0 && info('eva:attach-2-fill', attachments)}
            </Box>
            <Box component="span" sx={{ flexGrow: 1 }} />
            {assignees.length ? (
              <AvatarGroup max={3} sx={{ [`& .${avatarGroupClasses.avatar}`]: { width: 'var(--sp-3)', height: 'var(--sp-3)', fontSize: 'var(--fs-caption)' } }}>
                {assignees.map((u) => (
                  <Avatar key={u.id} alt={u.name} src={u.avatarUrl}>
                    {u.name.slice(0, 1)}
                  </Avatar>
                ))}
              </AvatarGroup>
            ) : null}
          </Box>
        ) : null}
      </Box>
    </KanbanItemRoot>
  );
}
