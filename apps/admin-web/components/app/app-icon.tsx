"use client";

import { Icon } from "@iconify/react";

import Box from "@mui/material/Box";
import type { SxProps, Theme } from "@mui/material/styles";

import { Iconify, allIconNames, type IconifyName } from "@/components/minimal/iconify";
import { registerExtraIcons, type ExtraIconName } from "@/components/app/iconify-extra";

/**
 * One icon from the template set OR the extra offline registry (components/app/iconify-extra.ts).
 * A template name renders the verbatim template `Iconify`; an extra name registers the extra
 * collection offline (no network fetch, no "loaded online" warning) and renders the same box
 * (inline-flex, width = height, flex-shrink 0) the template Iconify draws.
 */
export function AppIcon({ icon, width = 20, sx }: { icon: IconifyName | ExtraIconName; width?: number; sx?: SxProps<Theme> }) {
  if ((allIconNames as readonly string[]).includes(icon)) return <Iconify icon={icon as IconifyName} width={width} sx={sx} />;
  registerExtraIcons();
  return (
    <Box
      component={Icon}
      icon={icon}
      ssr
      sx={[{ width, height: width, flexShrink: 0, display: "inline-flex" }, ...(Array.isArray(sx) ? sx : sx ? [sx] : [])]}
    />
  );
}
