import type { CSSProperties, ReactNode } from "react";

import type { Theme, SxProps } from "@mui/material/styles";

import { EmptyContent } from "@/components/minimal/empty-content";

/**
 * In-card empty state = the template's EmptyContent as the template's TableNoData renders it: filled
 * (dashed tint box) with the template empty illustration. `icon` is accepted for older callers and no
 * longer drawn (the template shows its illustration, not a line icon). Pass `filled={false}` where the
 * empty state already sits inside a tinted surface.
 * Server-safe entry: the template component is `'use client'` and Next.js accepts server components
 * rendering client components.
 */
export function EmptyState({
  title,
  filled = true,
  className,
  style,
  description,
  action,
  sx,
}: {
  title?: ReactNode;
  icon?: ReactNode;
  filled?: boolean;
  className?: string;
  style?: CSSProperties;
  description?: ReactNode;
  action?: ReactNode;
  sx?: SxProps<Theme>;
}) {
  return (
    <EmptyContent
      className={className}
      style={style}
      filled={filled}
      title={title as string}
      description={description as string}
      action={action}
      sx={[{ py: 5, minHeight: 0, height: "auto" }, ...(Array.isArray(sx) ? sx : sx ? [sx] : [])]}
      slotProps={{ img: { alt: "" } }}
    />
  );
}
