"use client";

import { useEffect } from "react";

export function HashSectionScroller({ id }: { id: string }) {
  useEffect(() => {
    if (window.location.hash !== `#${id}`) return;

    let cancelled = false;
    let frame = 0;
    const scroll = () => {
      if (cancelled) return;
      document.getElementById(id)?.scrollIntoView({ block: "start" });
    };

    frame = window.requestAnimationFrame(scroll);
    const timeout = window.setTimeout(scroll, 120);

    return () => {
      cancelled = true;
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timeout);
    };
  }, [id]);

  return null;
}
