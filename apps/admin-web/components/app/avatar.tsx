import type { CSSProperties } from "react";

import type { Theme, SxProps } from "@mui/material/styles";
import MuiAvatar from "@mui/material/Avatar";
import MuiAvatarGroup, { avatarGroupClasses } from "@mui/material/AvatarGroup";

function deriveInitials(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "?";
  return (
    trimmed
      .split(/\s+/)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase() ?? "")
      .join("") || "?"
  );
}

/**
 * Thin wrapper on the template MUI Avatar with product behaviour:
 * derives initials from `name`, exposes `decorative` for aria-hidden inside
 * label groups, and sizes through a CSS var so nested icons/text scale in one
 * place. Style comes from the template theme; the wrapper adds no CSS module.
 */
export function Avatar({
  name,
  initials,
  src,
  size = 40,
  variant = "circular",
  decorative,
  className,
  style,
  sx,
}: {
  name: string;
  initials?: string;
  src?: string;
  size?: number;
  variant?: "circular" | "rounded";
  decorative?: boolean;
  className?: string;
  style?: CSSProperties;
  sx?: SxProps<Theme>;
}) {
  const text = initials?.trim() || deriveInitials(name);
  return (
    <MuiAvatar
      variant={variant}
      src={src}
      alt={decorative ? "" : name}
      title={name}
      className={className}
      role={decorative ? undefined : "img"}
      aria-hidden={decorative || undefined}
      aria-label={decorative ? undefined : name}
      style={{ ["--av" as string]: `${size}px`, ...style }}
      sx={[
        {
          width: "var(--av)",
          height: "var(--av)",
          fontSize: "calc(var(--av) * 0.45)",
          fontWeight: 500,
          lineHeight: 1,
        },
        ...(Array.isArray(sx) ? sx : sx ? [sx] : []),
      ]}
    >
      {src ? null : text}
    </MuiAvatar>
  );
}

/**
 * Thin wrapper on the template MUI AvatarGroup with product behaviour: renders
 * up to `max` avatars from `names`, folds `extra` server-counted owners into
 * the "+N" surplus slot, and can render an `empty` placeholder (with `danger`
 * tone) when there are no names at all. Sizing flows through the same CSS var
 * as Avatar so every child stays in step.
 */
export function AvatarGroup({
  names,
  max = 4,
  extra = 0,
  size = 28,
  empty,
  emptyTone,
  className,
  title,
}: {
  names: string[];
  max?: number;
  extra?: number;
  size?: number;
  empty?: string;
  emptyTone?: "danger";
  className?: string;
  title?: string;
}) {
  const total = names.length + extra;
  const overflow = total > max;
  const shown = overflow ? names.slice(0, max - 1) : names.slice(0, max);
  const surplus = total - shown.length;
  return (
    <MuiAvatarGroup
      max={100}
      className={className}
      title={title}
      style={{ ["--av" as string]: `${size}px` }}
      sx={{
        [`& .${avatarGroupClasses.avatar}`]: {
          width: "var(--av)",
          height: "var(--av)",
          fontSize: "calc(var(--av) * 0.45)",
          borderColor: "background.paper",
        },
      }}
    >
      {shown.map((n) => (
        <Avatar key={n} name={n} size={size} decorative />
      ))}
      {surplus > 0 ? (
        <MuiAvatar aria-label={`+${surplus}`} sx={{ bgcolor: "primary.lighter", color: "primary.dark", fontWeight: 600 }}>
          +{surplus}
        </MuiAvatar>
      ) : null}
      {names.length === 0 && empty ? (
        <MuiAvatar
          sx={{
            bgcolor: emptyTone === "danger" ? "error.lighter" : "primary.lighter",
            color: emptyTone === "danger" ? "error.dark" : "primary.dark",
            fontWeight: 600,
          }}
        >
          {empty}
        </MuiAvatar>
      ) : null}
    </MuiAvatarGroup>
  );
}
