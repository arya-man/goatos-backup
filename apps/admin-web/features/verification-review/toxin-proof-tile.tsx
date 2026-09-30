"use client";

// One toxin procedure proof, rendered IN PLACE under its step (maintainer request 2026-09-28).
//
// The drawer used to show every step's proof as an "Open proof" link to a new tab, so the CEO saw
// seven links and no evidence. The proofs are now on screen the moment the drawer opens:
//
//   - a PHOTO (the strip reading) is shown straight away -- one still, for the one test the
//     reviewer opened, never a click-to-reveal tile (docs/decisions/proof-photo-shown-on-open.md).
//     A tap on it ENLARGES it in a new tab; it never reveals it.
//   - a VIDEO is a player tile in place with a play button. No video bytes move until that button
//     is pressed (remote video must not auto-prepare or poster-probe); pressing it mounts the
//     <video> and plays it right there, no new tab.
//
// `src` is always the backend proof ROUTE (/api/proof-media/{proof_ref}), never a signed URL: the
// route signs per request, so nothing here is keyed by a rotating URL, and each tile is keyed by the
// stable proof_ref by its caller.
import { useState } from "react";
import { faro } from "@grafana/faro-web-sdk";
import Box from "@mui/material/Box";
import ButtonBase from "@mui/material/ButtonBase";
import Typography from "@mui/material/Typography";
import type { Theme } from "@mui/material/styles";
import { Iconify } from "@/components/minimal/iconify";

export type ToxinProofKind = "photo" | "video";

// A fixed 4:3 stage, never the clip's intrinsic size -- phone proofs are portrait and would run the
// dialog off the screen. The media is pinned (absolute + inset 0) and letterboxed with contain, the
// same rule the verify drawer's photo carries. Template anatomy: an outlined rounded media box
// (file-manager / product image tiles), sx only -- no legacy CSS (guard: toxin-proof-tile-sx).
const STAGE_SX = {
  position: "relative",
  width: 1,
  // 340px (template 8px grid x 42.5): a phone proof stays a readable tile, never full-bleed.
  maxWidth: (theme: Theme) => theme.spacing(42.5),
  aspectRatio: "4 / 3",
  mt: 1,
  border: 1,
  borderColor: "divider",
  borderRadius: "var(--r-md)",
  overflow: "hidden",
  bgcolor: "common.black",
  "& img, & video": { position: "absolute", inset: 0, width: "100%", height: "100%", display: "block", objectFit: "contain", bgcolor: "common.black" },
} as const;

export function ToxinProofTile({
  kind,
  src,
  proofRef,
  label,
  playLabel,
}: {
  kind: ToxinProofKind;
  src: string;
  proofRef: string;
  /** The step title, used as the image alt / video label. Backend copy. */
  label: string;
  /** Backend copy for the play button (drawer.media.play_video). */
  playLabel: string;
}) {
  const [playing, setPlaying] = useState(false);

  const report = (action: string): void => {
    // TELEMETRY GUARDRAIL: which proof the reviewer played / enlarged. Faro must never break the page.
    try {
      faro.api?.pushEvent("toxin_review_proof", { action, kind, proof_ref: proofRef });
    } catch {
      // Faro must never break the page.
    }
  };

  if (kind === "photo") {
    return (
      <Box sx={STAGE_SX} data-proof-kind="photo">
        {/* admin-proof-media-egress:ignore one strip photo for the one toxin test the reviewer opened, shown on open by decision proof-photo-shown-on-open; the drawer never renders a list of tests' media. */}
        <Box component="a" href={src} target="_blank" rel="noreferrer" onClick={() => report("photo_enlarged")} sx={{ position: "absolute", inset: 0, display: "block", cursor: "zoom-in" }}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={src} alt={label} loading="lazy" />
        </Box>
      </Box>
    );
  }

  return (
    <Box sx={STAGE_SX} data-proof-kind="video">
      {playing ? (
        // admin-proof-media-egress:ignore the reviewer pressed play on this one step's video; nothing is fetched before that press.
        <video src={src} controls autoPlay playsInline preload="metadata" aria-label={label} />
      ) : (
        <ButtonBase
          type="button"
          onClick={() => {
            setPlaying(true);
            report("video_played");
          }}
          sx={{
            position: "absolute",
            inset: 0,
            flexDirection: "column",
            gap: 1.25,
            color: "common.white",
            bgcolor: "grey.900",
            transition: (theme) => theme.transitions.create("background-color"),
            "&:hover": { bgcolor: "grey.800" },
          }}
        >
          <Iconify icon="solar:play-circle-bold" width={48} sx={{ color: "primary.light" }} />
          <Typography variant="subtitle2" component="span">{playLabel}</Typography>
        </ButtonBase>
      )}
    </Box>
  );
}
