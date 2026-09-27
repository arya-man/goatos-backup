"use client";

// Template user-list "New user" button (`<Button component={RouterLink} variant="contained" color="primary"
// startIcon={<Iconify icon="mingcute:add-line" />}>`), opening the Add Person drawer through the
// local-overlay link so the open never costs a route request.
import Button from "@mui/material/Button";

import { Iconify } from "@/components/minimal/iconify";
import { LocalOverlayLink } from "@/components/local-overlay-link";

export function PeopleAddButton({ href, label }: { href: string; label: string }) {
  return (
    <Button
      component={LocalOverlayLink}
      href={href}
      scroll={false}
      aria-haspopup="dialog"
      variant="contained" color="primary"
      startIcon={<Iconify icon="mingcute:add-line" />}
      sx={{ minHeight: "var(--tap-min)", whiteSpace: "nowrap" }}
    >
      {label}
    </Button>
  );
}
