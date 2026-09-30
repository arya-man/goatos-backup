"use client";

import { useEffect, useRef, useState } from "react";
import ButtonBase from "@mui/material/ButtonBase";
import { Iconify } from "@/components/minimal/iconify";

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
    <ButtonBase
      className="mzai-copy-ic"
      aria-label={done ? "Copied" : label}
      title={done ? "Copied" : label}
      onClick={() => void copy()}
    >
      {done ? <Iconify icon="eva:checkmark-fill" width={14} /> : <Iconify icon="solar:copy-bold" width={14} />}
    </ButtonBase>
  );
}
