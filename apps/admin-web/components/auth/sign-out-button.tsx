"use client";

import { useState } from "react";
import Button from "@mui/material/Button";
import { Iconify } from "@/components/minimal/iconify";
import { LOGIN_PATH } from "@/lib/auth/session-cookie";
import { clearFirebaseSession } from "@/lib/auth/firebase-client";

/** The account popover's sign-out row: the template layouts/components/sign-out-button (MUI Button, error, text in the popover). */
export function SignOutButton() {
  const [pending, setPending] = useState(false);

  return (
    <Button
      fullWidth
      color="error"
      variant="text"
      title="Sign out"
      aria-label="Sign out"
      disabled={pending}
      startIcon={<Iconify icon="ic:round-power-settings-new" />}
      onClick={() => {
        setPending(true);
        void clearFirebaseSession().finally(() => {
          window.location.assign(LOGIN_PATH);
        });
      }}
      sx={{ justifyContent: "flex-start" }}
    >
      {pending ? "Signing out" : "Sign out"}
    </Button>
  );
}
