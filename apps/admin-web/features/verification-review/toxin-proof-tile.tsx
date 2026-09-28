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
import { PlayCircle } from "lucide-react";

export type ToxinProofKind = "photo" | "video";

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
      <div className="toxin-proof photo">
        {/* admin-proof-media-egress:ignore one strip photo for the one toxin test the reviewer opened, shown on open by decision proof-photo-shown-on-open; the drawer never renders a list of tests' media. */}
        <a href={src} target="_blank" rel="noreferrer" className="toxin-proof-link" onClick={() => report("photo_enlarged")}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={src} alt={label} loading="lazy" />
        </a>
      </div>
    );
  }

  return (
    <div className="toxin-proof video">
      {playing ? (
        // admin-proof-media-egress:ignore the reviewer pressed play on this one step's video; nothing is fetched before that press.
        <video src={src} controls autoPlay playsInline preload="metadata" aria-label={label} />
      ) : (
        <button
          type="button"
          className="toxin-proof-play"
          onClick={() => {
            setPlaying(true);
            report("video_played");
          }}
        >
          <span className="vr-media-open-mark">
            <PlayCircle className="ic" aria-hidden="true" />
          </span>
          {playLabel}
        </button>
      )}
    </div>
  );
}
