"use client";

import { AnimatePresence, m, useReducedMotion } from "motion/react";
import IconButton from "@mui/material/IconButton";
import { varTap, varHover, transitionTap } from "@/layouts/template/animate";
import { Moon, Sun } from "lucide-react";
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
  const reduce = useReducedMotion();
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
      title={label}
      aria-label={label}
      onClick={() => {
        const next: ThemeMode = isLight ? "dark" : "light";
        applyTheme(next);
        onChange?.(next);
      }}
    >
      <AnimatePresence mode="wait" initial={false}>
        <m.span
          key={mode}
          style={{ display: "grid", placeItems: "center" }}
          initial={reduce ? false : { rotate: -90, opacity: 0, scale: 0.8 }}
          animate={{ rotate: 0, opacity: 1, scale: 1 }}
          exit={reduce ? undefined : { rotate: 90, opacity: 0, scale: 0.8 }}
          transition={{ duration: 0.25, ease: [0.4, 0, 0.2, 1] }}
        >
          {isLight ? <Moon width={24} height={24} /> : <Sun width={24} height={24} />}
        </m.span>
      </AnimatePresence>
    </IconButton>
  );
}
