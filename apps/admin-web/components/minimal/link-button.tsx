'use client';

// The template's `<Button component={RouterLink} href=…>` (used across the template's list and detail
// views), as a client leaf so server pages can render it with a plain href.
import type { ButtonProps } from '@mui/material/Button';

import Button from '@mui/material/Button';

import Link from '@/components/no-prefetch-link';

export type LinkButtonProps = Omit<ButtonProps<typeof Link>, 'component'> & {
  href: string;
  replace?: boolean;
  scroll?: boolean;
};

export function LinkButton({ href, replace, scroll, ...other }: LinkButtonProps) {
  return <Button component={Link} href={href} replace={replace} scroll={scroll} {...other} />;
}
