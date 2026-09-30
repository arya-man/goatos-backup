"use client";

import { Iconify } from "@/components/minimal/iconify";
import { useCallback, useState, useTransition } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { PersonAccess } from "@/lib/api/server";
import { loadPersonAccessAction } from "./access-actions";
import { PersonAccessModal } from "./person-access-modal";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import Dialog from "@mui/material/Dialog";
import useMediaQuery from "@mui/material/useMediaQuery";
import type { Theme } from "@mui/material/styles";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";

/**
 * Opens the access editor for one directory row.
 *
 * The overlay opens IMMEDIATELY and loads its own detail, rather than the page
 * fetching access for every row it renders. Two reasons, both rules rather than
 * preferences: an overlay toggle must not navigate or trigger a page-level load
 * (the local-overlay contract), and pre-loading access for 25 rows would be 25
 * reads to render one screen nobody may open.
 */
export function PersonAccessLauncher({
  personId,
  personName,
  pageContract,
  compact = false,
}: {
  personId: string;
  personName: string;
  pageContract: AdminUiPageContract;
  /** Phone rows: a 44px icon button instead of the labelled text button. */
  compact?: boolean;
}) {
  const [access, setAccess] = useState<PersonAccess | null>(null);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  // Template dialogs go full screen below sm (the matrix is unreadable in a 358px card).
  const phone = useMediaQuery((theme: Theme) => theme.breakpoints.down("sm"));

  const openEditor = useCallback(() => {
    setOpen(true);
    setError("");
    startTransition(async () => {
      const result = await loadPersonAccessAction(personId);
      if (!result.ok) {
        setError(result.message);
        return;
      }
      setAccess(result.access);
    });
  }, [personId]);

  const close = useCallback(() => {
    setOpen(false);
    // The loaded record is DISCARDED on close, so reopening always re-reads. Access
    // is exactly the state where a cached view can be quietly out of date after
    // someone else's edit.
    setAccess(null);
    setError("");
  }, []);

  return (
    <>
      {compact ? (
        <IconButton
          onClick={openEditor}
          title={copy(pageContract, "access.open_hint")}
          aria-label={`${copy(pageContract, "access.open")} — ${personName}`}
          sx={{ width: "var(--tap-min)", height: "var(--tap-min)", flexShrink: 0 }}
        >
          <Iconify icon="solar:shield-keyhole-bold-duotone" width={20} />
        </IconButton>
      ) : (
        <Button
          size="small"
          color="inherit"
          onClick={openEditor}
          title={copy(pageContract, "access.open_hint")}
          aria-label={`${copy(pageContract, "access.open")} — ${personName}`}
          startIcon={<Iconify icon="solar:shield-keyhole-bold-duotone" width={18} />}
          sx={{ whiteSpace: "nowrap" }}
        >
          {copy(pageContract, "access.open")}
        </Button>
      )}

      {/* One template MUI Dialog for the loading state and the editor: it portals to <body> (a
          transformed tab panel can no longer capture it), traps focus, closes on Escape / scrim
          and returns focus to this Access button. */}
      <Dialog
        open={open}
        onClose={close}
        fullWidth
        fullScreen={phone}
        maxWidth="md"
        scroll="paper"
        slotProps={{ paper: { "aria-label": access ? `Access for ${access.display_name}` : personName } }}
      >
        {access ? (
          <PersonAccessModal access={access} pageContract={pageContract} onClose={close} />
        ) : (
          <>
          <DialogTitle sx={{ display: "flex", alignItems: "center", gap: 1, pr: 1.5 }}>
            <Box component="span" sx={{ flexGrow: 1, minWidth: 0 }}>{personName}</Box>
            <IconButton onClick={close} aria-label={copy(pageContract, "action.close")} sx={{ width: "var(--tap-min)", height: "var(--tap-min)" }}>
              <Iconify icon="mingcute:close-line" />
            </IconButton>
          </DialogTitle>
          <DialogContent>
            {error ? (
              <Alert severity="error" role="alert">
                {error}
              </Alert>
            ) : (
              <Typography variant="body2" aria-live="polite" sx={{ py: 2 }}>
                {pending ? copy(pageContract, "access.loading") : copy(pageContract, "access.error.load")}
              </Typography>
            )}
          </DialogContent>
          </>
        )}
      </Dialog>
    </>
  );
}
