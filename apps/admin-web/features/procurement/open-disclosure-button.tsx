"use client";

import { Plus } from "lucide-react";
import Button from "@mui/material/Button";

/**
 * Header primary that opens an accordion (`<details id>`) further down the page and scrolls it
 * into view: the write form stays where it is, but the action is where a primary belongs.
 */
export function OpenDisclosureButton({ targetId, label }: { targetId: string; label: string }) {
  return (
    <Button
      type="button"
      variant="contained"
      color="primary"
      startIcon={<Plus className="ic" aria-hidden="true" />}
      onClick={() => {
        const details = document.getElementById(targetId) as HTMLDetailsElement | null;
        if (!details) return;
        details.open = true;
        details.scrollIntoView({ behavior: "smooth", block: "start" });
        details.querySelector<HTMLElement>("input, select")?.focus({ preventScroll: true });
      }}
    >
      {label}
    </Button>
  );
}
