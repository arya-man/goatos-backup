"use client";

import Button from "@mui/material/Button";
import { Iconify } from "@/components/minimal/iconify";

/** Reloads the current page. Read-only: it only re-requests what is already on screen. */
export function RetryButton({ label }: { label: string }) {
  return (
    <Button
      variant="outlined"
      color="inherit"
      startIcon={<Iconify icon="solar:restart-bold" />}
      onClick={() => window.location.reload()}
    >
      {label}
    </Button>
  );
}
