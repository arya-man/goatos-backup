import type { ReactNode } from "react";
import { cx, toneVars, type KitTone } from "@/lib/tone";

export type IconBadgeProps = {
  icon: ReactNode;
  tone?: KitTone;
  /** sm 36px, md 48px, lg 56px */
  size?: "sm" | "md" | "lg";
  shape?: "rounded" | "circle";
  className?: string;
};

export function IconBadge({ icon, tone = "primary", size = "md", shape = "rounded", className }: IconBadgeProps) {
  const t = toneVars(tone);
  return (
    <span
      aria-hidden="true"
      className={cx("kit-iconbadge", `kit-iconbadge-${size}`, shape === "circle" && "kit-round", className)}
      style={{ background: t.soft, color: t.ink }}
    >
      {icon}
    </span>
  );
}
