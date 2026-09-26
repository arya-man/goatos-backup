import type { CSSProperties, ReactNode } from "react";

import type { Theme, SxProps } from "@mui/material/styles";

import { EmptyContent } from "@/components/minimal/empty-content";
import { Iconify } from "@/layouts/template/iconify";

/**
 * Compact in-card empty state. Renders the template's EmptyContent with
 * compact spacing via sx theme tokens (never our own CSS). When `icon` is
 * given, the template's img slot is swapped to a span containing the icon
 * (Iconify glyphs or an inline svg); otherwise a small default glyph shows.
 * Server-safe entry: the template component is `'use client'` and Next.js
 * accepts server components rendering client components.
 */
export function EmptyState({
  title,
  icon,
  filled,
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
  const iconNode = icon ?? <Iconify icon="solar:chart-square-outline" width="var(--sp-5)" />;
  return (
    <EmptyContent
      className={className}
      style={style}
      filled={filled}
      title={title as string}
      description={description as string}
      action={action}
      sx={[
        { py: 3, minHeight: 0, height: "auto" },
        ...(Array.isArray(sx) ? sx : sx ? [sx] : []),
      ]}
      slotProps={{
        img: {
          component: "span",
          src: undefined,
          alt: "",
          children: iconNode,
          sx: {
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            width: "var(--sp-5)",
            height: "var(--sp-5)",
            maxWidth: "unset",
            color: "text.disabled",
            "& svg": { width: "var(--sp-5)", height: "var(--sp-5)" },
            "& .ic": { width: "var(--sp-5)", height: "var(--sp-5)" },
          },
        },
        title: { variant: "h6", sx: { mt: 1, color: "text.secondary" } },
        description: { variant: "body2", sx: { mt: 0.5, color: "text.disabled" } },
      }}
    />
  );
}
