"use client";

import { useState } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";

import { resolveVendorVoiceNoteUrl } from "./vendor-actions";

/**
 * Plays a vendor's voice note (an `audio` proof recorded on the phone).
 *
 * The signed download URL is short-lived and per-caller, so it is resolved on demand -- when the
 * reader asks to hear the note -- rather than for every vendor on the page. An unresolvable note
 * renders the backend's copy, never a broken player: the ref exists, the bytes may not be
 * reachable right now.
 */
export function VendorVoiceNote({
  proofRef,
  loadLabel,
  unavailableCopy,
}: {
  proofRef: string;
  loadLabel: string;
  unavailableCopy: string;
}) {
  const [src, setSrc] = useState<string | null>(null);
  const [state, setState] = useState<"idle" | "loading" | "failed">("idle");

  async function load() {
    setState("loading");
    try {
      const url = await resolveVendorVoiceNoteUrl(proofRef);
      if (url) {
        setSrc(url);
        setState("idle");
      } else {
        setState("failed");
      }
    } catch {
      setState("failed");
    }
  }

  if (src) {
    return (
      <Box component="audio" controls preload="metadata" src={src} sx={{ width: 1 }} onError={() => setState("failed")}>
        {unavailableCopy}
      </Box>
    );
  }
  if (state === "failed") {
    return (
      <Typography variant="body2" sx={{ color: "text.secondary" }}>
        {unavailableCopy}
      </Typography>
    );
  }
  return (
    <Button type="button" variant="outlined" color="inherit" loading={state === "loading"} onClick={load} sx={{ alignSelf: "flex-start" }}>
      {loadLabel}
    </Button>
  );
}
