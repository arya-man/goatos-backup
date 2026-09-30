"use client";

import { useEffect, useRef, useState } from "react";
import IconButton from "@mui/material/IconButton";
import { Iconify } from "@/components/minimal/iconify";

// The answer / code-block Copy: a template small IconButton (the chat message item's action slot).

export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const copy = async () => {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        // Insecure-context webviews have no async clipboard: textarea fallback.
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.setAttribute("readonly", "");
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        const ok = document.execCommand("copy");
        ta.remove();
        if (!ok) return;
      }
    } catch {
      return;
    }
    setDone(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setDone(false), 1500);
  };
  return (
    <IconButton
      size="small"
      color="inherit"
      aria-label={done ? "Copied" : label}
      title={done ? "Copied" : label}
      onClick={() => void copy()}
    >
      <Iconify icon={done ? "eva:checkmark-fill" : "solar:copy-bold"} width={16} />
    </IconButton>
  );
}
