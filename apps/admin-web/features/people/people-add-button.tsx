"use client";

// Template user-list "New user" button (`<Button component={RouterLink} variant="contained" color="primary"
// startIcon={<Iconify icon="mingcute:add-line" />}>`), opening the Add Person drawer through the
// local-overlay link so the open never costs a route request.
import Button from "@mui/material/Button";

import { Iconify } from "@/components/minimal/iconify";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import Link from "@/components/no-prefetch-link";

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
      // 36px like every other header primary on desktop (J2B P2-10: it stood 44px); the 44px tap
      // floor applies where AppBaseline applies it to buttons (phones / coarse pointers) -- this is
      // an <a>, which that floor does not reach, so it restates it (phone width + coarse pointer).
      sx={{ whiteSpace: "nowrap", minHeight: { xs: "var(--tap-min)", sm: "auto" }, "@media (pointer: coarse)": { minHeight: "var(--tap-min)" } }}
    >
      {label}
    </Button>
  );
}
