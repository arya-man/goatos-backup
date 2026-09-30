import type { ReactNode } from "react";

import Box from "@mui/material/Box";

import { toneVars, type KitTone } from "@/lib/tone";

export type IconBadgeProps = {
  icon: ReactNode;
  tone?: KitTone;
  /** sm 36px, md 48px, lg 56px */
  size?: "sm" | "md" | "lg";
  shape?: "rounded" | "circle";
  className?: string;
};

// Template icon tile (the widget-summary corner icon): soft tone fill, icon at half the tile.
const SIDE = { sm: 4.5, md: 6, lg: 7 } as const;

export function IconBadge({ icon, tone = "primary", size = "md", shape = "rounded", className }: IconBadgeProps) {
  const t = toneVars(tone);
  return (
    <Box
      component="span"
      aria-hidden="true"
      className={className}
      sx={(theme) => ({
        display: "grid",
        placeItems: "center",
        flex: "none",
        width: theme.spacing(SIDE[size]),
        height: theme.spacing(SIDE[size]),
        borderRadius: shape === "circle" ? "50%" : "calc(1.5 * var(--shape-borderRadius))",
        bgcolor: t.soft,
        color: t.ink,
        "& svg": { width: "50%", height: "50%" },
      })}
    >
      {icon}
    </Box>
  );
}
