"use client";

// Template user-list "New user" button (`<Button component={RouterLink} variant="contained" color="primary"
// startIcon={<Iconify icon="mingcute:add-line" />}>`), opening the Add Person drawer through the
// local-overlay link so the open never costs a route request.
import Button from "@mui/material/Button";

import { Iconify } from "@/components/minimal/iconify";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import Link from "@/components/no-prefetch-link";
import { TAP_MIN } from "@/theme/tap-target";

/** `overlay`: the drawer is on this page (All People), so the open never costs a route request. */
export function PeopleAddButton({ href, label, overlay = true }: { href: string; label: string; overlay?: boolean }) {
  return (
    <Button
      component={overlay ? LocalOverlayLink : Link}
      href={href}
      scroll={false}
      aria-haspopup="dialog"
      variant="contained" color="primary"
      startIcon={<Iconify icon="mingcute:add-line" />}
      sx={{ minHeight: TAP_MIN, whiteSpace: "nowrap" }}
    >
      {label}
    </Button>
  );
}
