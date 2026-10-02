'use client';

// Mesha kanban adapter (components/app, NOT a template file) built on the MUI Minimal template (sections/kanban/column/styles.tsx, with the state
// class names from sections/kanban/classes.ts inlined).
// Changes: ColumnWrapper is a plain flex item (no framer-motion `m.li`, the page owns no column
// drag), so it carries the column width itself and never exceeds a phone viewport; ColumnList does
// not scroll on its own (columns grow with their cards and the page pages them), so a phone never
// gets a scroll trap inside the board; the column-over state keeps the cards visible (it marks a
// legal drop target for a dragged card, not a dragged column).
import type { CSSObject } from '@mui/material/styles';

import { varAlpha } from 'minimal-shared/utils';

import { styled } from '@mui/material/styles';

// ----------------------------------------------------------------------

export const kanbanColumnState = {
  dragging: '--dragging',
  taskOver: '--task-over',
  columnOver: '--column-over',
} as const;

export const ColumnWrapper = styled('section')(({ theme }) => ({
  flexShrink: 0,
  display: 'flex',
  flexDirection: 'column',
  width: 'min(var(--kanban-column-width), calc(100vw - calc(3 * var(--spacing)) * 2))',
  // Phone: the lanes stack (KanbanBoard), each filling the board.
  [theme.breakpoints.down('sm')]: { width: '100%' },
}));

export const ColumnRoot = styled('div')(({ theme }) => {
  const backgroundOverStyles: Record<'idle' | 'taskOver' | 'columnOver', CSSObject> = {
    idle: {
      '--background-over': varAlpha(theme.vars.palette.grey['500Channel'], 0.08),
      ...theme.applyStyles('dark', {
        '--background-over': varAlpha(theme.vars.palette.grey['500Channel'], 0.16),
      }),
      top: 0,
      left: 0,
      content: '""',
      width: '100%',
      height: '100%',
      borderWidth: '1px',
      position: 'absolute',
      pointerEvents: 'none',
      borderRadius: 'inherit',
      backgroundColor: 'transparent',
      transition: theme.transitions.create(['background-color']),
    },
    taskOver: {
      borderStyle: 'solid',
      backgroundColor: 'var(--background-over)',
      borderColor: varAlpha(theme.vars.palette.grey['500Channel'], 0.08),
    },
    columnOver: {
      borderStyle: 'dashed',
      backgroundColor: 'var(--background-over)',
      borderColor: varAlpha(theme.vars.palette.grey['500Channel'], 0.24),
    },
  };

  return {
    display: 'flex',
    position: 'relative',
    flexDirection: 'column',
    gap: 'var(--kanban-item-gap)',
    borderRadius: 'var(--kanban-column-radius)',
    backgroundColor: theme.vars.palette.background.neutral,
    '&::before': backgroundOverStyles.idle,
    [`&.${kanbanColumnState.dragging}`]: {
      opacity: 0.4,
    },
    [`&.${kanbanColumnState.taskOver}`]: {
      '&::before': backgroundOverStyles.taskOver,
    },
    [`&.${kanbanColumnState.columnOver}`]: {
      '&::before': backgroundOverStyles.columnOver,
    },
  };
});

export const ColumnList = styled('ul')({
  margin: 0,
  minHeight: 'calc(10 * var(--spacing))',
  display: 'flex',
  listStyle: 'none',
  overflowAnchor: 'none',
  flexDirection: 'column',
  gap: 'var(--kanban-item-gap)',
  paddingLeft: 'var(--kanban-column-px)',
  paddingRight: 'var(--kanban-column-px)',
  paddingBottom: 'var(--kanban-column-pb)',
});
