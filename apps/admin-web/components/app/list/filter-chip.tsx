'use client';

// A URL filter chip: the template's clickable MUI Chip (filled when active, outlined otherwise), as a
// link so the choice lives in the address bar. Serializable props for server pages.
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';

import Link from '@/components/no-prefetch-link';

import { Iconify } from '@/components/minimal/iconify';

export type FilterChipProps = {
  label: React.ReactNode;
  href?: string;
  on?: boolean;
  /** Small colour dot before the label (legend chips). */
  dot?: string;
  disabled?: boolean;
  title?: string;
  replace?: boolean;
  /** An applied filter: the link removes it, drawn with the template chip's delete icon. */
  removable?: boolean;
  className?: string;
};

export function FilterChip({ label, href, on = false, dot, disabled = false, title, replace, removable = false, className }: FilterChipProps) {
  const icon = dot ? <Box component="span" sx={{ width: 'var(--sp-1)', height: 'var(--sp-1)', borderRadius: '50%', bgcolor: dot, ml: 1 }} /> : undefined;
  if (!href || disabled) {
    return <Chip label={label} icon={icon} disabled={disabled} title={title} aria-disabled={disabled || undefined} variant={on ? 'filled' : 'outlined'} className={className} />;
  }
  return (
    <Chip
      clickable
      component={Link}
      href={href}
      scroll={false}
      replace={replace}
      label={label}
      icon={icon}
      title={title}
      aria-pressed={on}
      color={on ? 'primary' : 'default'}
      variant={removable ? 'soft' : on ? 'filled' : 'outlined'}
      size={removable ? 'small' : 'medium'}
      {...(removable ? { onDelete: () => undefined, deleteIcon: <Iconify icon="solar:close-circle-bold" /> } : {})}
      className={className}
    />
  );
}
