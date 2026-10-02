"use client";

import { m } from "motion/react";
import IconButton from "@mui/material/IconButton";
import { varTap, varHover, transitionTap } from "@/layouts/template/animate";
import SvgIcon from "@mui/material/SvgIcon";
import { settingIcons } from "@/layouts/template/settings/drawer/icons";
import { AppIcon } from "@/components/app/app-icon";
import { useSyncExternalStore } from "react";
import { applyTheme, readTheme, type ThemeMode } from "@/lib/theme";

function subscribeTheme(onChange: () => void): () => void {
  if (typeof document === "undefined") return () => {};
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "data-theme"] });
  return () => observer.disconnect();
}
const serverTheme = (): ThemeMode => "dark";

export type ThemeToggleProps = {
  labelToLight: string;
  labelToDark: string;
  className?: string;
  onChange?: (mode: ThemeMode) => void;
};

export function ThemeToggle({ labelToLight, labelToDark, className, onChange }: ThemeToggleProps) {
  const mode = useSyncExternalStore(subscribeTheme, readTheme, serverTheme);
  const isLight = mode === "light";
  const label = isLight ? labelToDark : labelToLight;
  // The GLYPH is a pure function of html[data-theme], decided by CSS on the first paint (PR #294 L3/K9).
  // It used to be a keyed motion exit/enter swap: hydration rendered the server's "dark" guess
  // (sun) and then had to finish an exit before the moon entered, so a light page showed the sun or the
  // moon depending on when it was looked at. Both glyphs are always in the DOM; the boot script's
  // data-theme shows exactly one, before and after hydration, and a click only flips the attribute.
  // Template header IconButton idiom (layouts/components/settings-button: tap/hover motion, 24px icon);
  // it sits where the template header has its Settings button.
  return (
    <IconButton
      component={m.button}
      whileTap={varTap(0.96)}
      whileHover={varHover(1.04)}
      transition={transitionTap()}
      className={className}
      sx={{
        "& > span:not(.MuiTouchRipple-root)": { display: "grid", placeItems: "center" },
        "& [data-glyph]": { gridArea: "1 / 1", display: "grid", placeItems: "center" },
        // Default (no attribute yet, or dark): sun = "go light".
        "& [data-glyph='to-dark']": { display: "none" },
        ':root[data-theme="light"] & [data-glyph="to-dark"]': { display: "grid" },
        ':root[data-theme="light"] & [data-glyph="to-light"]': { display: "none" },
      }}
      title={label}
      aria-label={label}
      onClick={() => {
        const next: ThemeMode = isLight ? "dark" : "light";
        applyTheme(next);
        onChange?.(next);
      }}
    >
      <span>
        {/* Moon = go dark: the template settings-drawer glyph (verbatim settingIcons). Sun = go light:
            solar sun from the extra offline registry (components/app/iconify-extra.ts). */}
        <span data-glyph="to-dark" aria-hidden>
          <SvgIcon fontSize="medium">{settingIcons.moon}</SvgIcon>
        </span>
        <span data-glyph="to-light" aria-hidden>
          <AppIcon icon="solar:sun-bold" width={24} />
        </span>
      </span>
    </IconButton>
  );
}
