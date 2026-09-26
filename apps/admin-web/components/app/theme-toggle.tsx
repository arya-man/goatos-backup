"use client";

import { AnimatePresence, motion, useReducedMotion } from "motion/react";
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

export function ThemeToggle({ labelToLight, labelToDark, className = "iconbtn", onChange }: ThemeToggleProps) {
  const mode = useSyncExternalStore(subscribeTheme, readTheme, serverTheme);
  const reduce = useReducedMotion();
  const isLight = mode === "light";
  const label = isLight ? labelToDark : labelToLight;
  return (
    <button
      type="button"
      className={`${className} kit-press`}
      title={label}
      aria-label={label}
      onClick={() => {
        const next: ThemeMode = isLight ? "dark" : "light";
        applyTheme(next);
        onChange?.(next);
      }}
    >
      <AnimatePresence mode="wait" initial={false}>
        <motion.span
          key={mode}
          style={{ display: "grid", placeItems: "center" }}
          initial={reduce ? false : { rotate: -90, opacity: 0, scale: 0.8 }}
          animate={{ rotate: 0, opacity: 1, scale: 1 }}
          exit={reduce ? undefined : { rotate: 90, opacity: 0, scale: 0.8 }}
          transition={{ duration: 0.25, ease: [0.4, 0, 0.2, 1] }}
        >
          {isLight ? <Moon className="ic" /> : <Sun className="ic" />}
        </motion.span>
      </AnimatePresence>
    </button>
  );
}
