"use client";

import { useState } from "react";

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
      <audio controls preload="metadata" src={src} style={{ width: "100%" }} onError={() => setState("failed")}>
        {unavailableCopy}
      </audio>
    );
  }
  if (state === "failed") return <div className="note">{unavailableCopy}</div>;
  return (
    <button type="button" className="btn" disabled={state === "loading"} onClick={load}>
      {loadLabel}
    </button>
  );
}
