"use client";

import { AnimatePresence, m, useReducedMotion } from "motion/react";
import IconButton from "@mui/material/IconButton";
import { varTap, varHover, transitionTap } from "@/layouts/template/animate";
import SvgIcon from "@mui/material/SvgIcon";
import { settingIcons } from "@/layouts/template/settings/drawer/icons";
import { AppIcon } from "@/components/app/app-icon";
import { useState, useSyncExternalStore } from "react";
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
  // Animate only a swap the person asked for. Hydration corrects the server's "dark" guess on a light
  // page; animating THAT ran a 250ms sun-out / moon-in on every light page load, so a light page could
  // still be showing the sun (PR #294 L3). Until the first click the glyph simply is the mode's.
  const [toggled, setToggled] = useState(false);
  const reduce = useReducedMotion() || !toggled;
  const isLight = mode === "light";
  const label = isLight ? labelToDark : labelToLight;
  // Template header IconButton idiom (layouts/components/settings-button: tap/hover motion, 24px icon);
  // it sits where the template header has its Settings button.
  return (
    <IconButton
      component={m.button}
      whileTap={varTap(0.96)}
      whileHover={varHover(1.04)}
      transition={transitionTap()}
      className={className}
      sx={{ "& > span:not(.MuiTouchRipple-root)": { display: "grid", placeItems: "center" } }}
      title={label}
      aria-label={label}
      onClick={() => {
        const next: ThemeMode = isLight ? "dark" : "light";
        setToggled(true);
        applyTheme(next);
        onChange?.(next);
      }}
    >
      <AnimatePresence mode="wait" initial={false}>
        <m.span
          key={mode}
          initial={reduce ? false : { rotate: -90, opacity: 0, scale: 0.8 }}
          animate={{ rotate: 0, opacity: 1, scale: 1 }}
          exit={reduce ? { opacity: 0, transition: { duration: 0 } } : { rotate: 90, opacity: 0, scale: 0.8 }}
          transition={{ duration: 0.25, ease: [0.4, 0, 0.2, 1] }}
        >
          {/* Moon = go dark: the template settings-drawer glyph (verbatim settingIcons). Sun = go light:
              solar sun from the extra offline registry (components/app/iconify-extra.ts). */}
          {isLight ? <SvgIcon fontSize="medium">{settingIcons.moon}</SvgIcon> : <AppIcon icon="solar:sun-bold" width={24} />}
        </m.span>
      </AnimatePresence>
    </IconButton>
  );
}
