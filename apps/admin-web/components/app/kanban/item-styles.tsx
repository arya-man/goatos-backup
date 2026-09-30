'use client';

// Mesha kanban adapter (components/app, NOT a template file) built on the MUI Minimal template (sections/kanban/item/styles.tsx).
// Changes: ItemRoot / DropIndicator / ItemPreview / ItemImage are not copied (the item shell is
// components/app/kanban `KanbanItemRoot`, and pages own drag and drop); the IKanbanTask prop
// types are replaced by plain props (a count instead of an array, assignees as name + initial);
// ItemStatus renders nothing without a status; ItemInfo takes optional leading/trailing nodes so a
// page can put its own counts beside the template comment / attachment readings.
import type { BoxProps } from '@mui/material/Box';
import type { TypographyProps } from '@mui/material/Typography';

import Box from '@mui/material/Box';
import Avatar from '@mui/material/Avatar';
import { styled } from '@mui/material/styles';
import Typography from '@mui/material/Typography';
import AvatarGroup, { avatarGroupClasses } from '@mui/material/AvatarGroup';

import { Iconify } from '@/components/minimal/iconify';
import type { IconifyName, IconifyProps } from '@/components/minimal/iconify';

// ----------------------------------------------------------------------

export const ItemContent = styled('div')(({ theme }) => ({
  position: 'relative',
  padding: theme.spacing(2.5, 2),
}));

// ----------------------------------------------------------------------

export type ItemNameProps = TypographyProps & {
  name: React.ReactNode;
};

export function ItemName({ name, sx, ...other }: ItemNameProps) {
  return (
    <Typography
      noWrap
      component="span"
      variant="subtitle2"
      sx={[{ display: 'block' }, ...(Array.isArray(sx) ? sx : [sx])]}
      {...other}
    >
      {name}
    </Typography>
  );
}

// ----------------------------------------------------------------------

export type ItemStatusProps = Omit<IconifyProps, 'icon'> & {
  status?: 'low' | 'medium' | 'high' | null;
};

export function ItemStatus({ sx, status, ...other }: ItemStatusProps) {
  if (!status) return null;

  return (
    <Iconify
      icon={
        (status === 'low' && 'solar:double-alt-arrow-down-bold-duotone') ||
        (status === 'medium' && 'solar:double-alt-arrow-right-bold-duotone') ||
        'solar:double-alt-arrow-up-bold-duotone'
      }
      sx={[
        {
          top: 4,
          right: 4,
          position: 'absolute',
          color: 'error.main',
          ...(status === 'low' && { color: 'info.main' }),
          ...(status === 'medium' && { color: 'warning.main' }),
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    />
  );
}

// ----------------------------------------------------------------------

export type ItemInfoAssignee = { id: string; name: string; avatarUrl?: string; initial?: React.ReactNode; color?: 'default' | 'error' };

export type ItemInfoProps = Omit<BoxProps, 'children'> & {
  comments?: number;
  attachments?: number;
  assignee?: ItemInfoAssignee[];
  /** Mesha: extra readings on the left, after the comment / attachment counts. */
  children?: React.ReactNode;
  /** Mesha: tooltip text for the avatar group. */
  assigneeTitle?: string;
};

export function ItemInfo({ sx, assignee = [], comments = 0, attachments = 0, children, assigneeTitle, ...other }: ItemInfoProps) {
  const hasComments = comments > 0;
  const hasAssignee = !!assignee.length;
  const hasAttachments = attachments > 0;

  if (!hasComments && !hasAttachments && !hasAssignee && !children) return null;

  const renderInfo = (icon: IconifyName, count: number) => (
    <Box
      sx={{
        gap: 0.25,
        display: 'flex',
        alignItems: 'center',
        typography: 'caption',
        color: 'text.disabled',
      }}
    >
      <Iconify width={16} icon={icon} />
      <Box component="span">{count}</Box>
    </Box>
  );

  return (
    <Box
      sx={[
        {
          mt: 2,
          display: 'flex',
          alignItems: 'center',
          pointerEvents: 'none',
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      {(hasComments || hasAttachments || children) && (
        <Box sx={{ gap: 1, display: 'flex', alignItems: 'center', flexWrap: 'wrap', minWidth: 0 }}>
          {hasComments && renderInfo('solar:chat-round-dots-bold', comments)}
          {hasAttachments && renderInfo('eva:attach-2-fill', attachments)}
          {children}
        </Box>
      )}

      {hasAssignee && (
        <>
          <Box component="span" sx={{ flexGrow: 1 }} />
          <AvatarGroup
            title={assigneeTitle}
            sx={{
              flexShrink: 0,
              [`& .${avatarGroupClasses.avatar}`]: {
                width: 'calc(3 * var(--spacing))',
                height: 'calc(3 * var(--spacing))',
                typography: 'caption',
              },
            }}
          >
            {assignee.map((user) => (
              <Avatar
                key={user.id}
                alt={user.name}
                src={user.avatarUrl}
                sx={user.color === 'error' ? { bgcolor: 'error.main', color: 'error.contrastText' } : undefined}
              >
                {user.initial ?? user.name.slice(0, 1)}
              </Avatar>
            ))}
          </AvatarGroup>
        </>
      )}
    </Box>
  );
}
