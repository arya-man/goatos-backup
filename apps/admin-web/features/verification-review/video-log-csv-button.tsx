"use client";

import { Download, Loader2 } from "lucide-react";
import { useState, useTransition } from "react";

import { exportVideoLogAction } from "./video-log-export-action";

/**
 * Downloads the WHOLE DAY's video log as a CSV — every shed in scope, one row per video.
 *
 * Deliberately NOT limited to what is on screen (maintainer, 2026-08-15). The summary shows one
 * line per shed and the detail shows one shed, but the file people actually want is the day: every
 * video, with its shed, its work and the time it arrived.
 *
 * The rows are fetched ON CLICK through a server action rather than rendered with the panel. The
 * screen deliberately does not carry every row — a vaccination drive raises one item per animal, so
 * a park-day runs to thousands of proofs — and paying that on every panel open, for a file most
 * viewers never ask for, is the over-fetch the two-level design exists to avoid.
 */
export function VideoLogCsvButton({
  filename,
  label,
  businessDate,
  parkId,
  headers,
  awaitingLabel,
  truncatedNote,
}: {
  filename: string;
  label: string;
  businessDate: string;
  parkId?: string;
  /** Column headers in the screen's own backend-owned words. */
  headers: string[];
  awaitingLabel: string;
  /** Shown when the day exceeded the export bound, so a partial file never reads as a whole day. */
  truncatedNote: string;
}) {
  const [pending, startTransition] = useTransition();
  const [note, setNote] = useState("");

  function onClick(): void {
    setNote("");
    startTransition(async () => {
      const result = await exportVideoLogAction({ businessDate, parkId, headers, awaitingLabel });
      if (result.truncated) setNote(truncatedNote);
      writeCsv(filename, result.rows);
    });
  }

  return (
    <span className="vl-csv-wrap">
      <button type="button" className="btn sm vl-csv-btn" onClick={onClick} disabled={pending} aria-busy={pending}>
        {pending ? <Loader2 className="ic vl-csv-spin" aria-hidden="true" /> : <Download className="ic" aria-hidden="true" />}
        {label}
      </button>
      {/* No silent truncation: a capped export says so rather than looking like the whole day. */}
      {note ? <span className="small warn vl-csv-note">{note}</span> : null}
    </span>
  );
}

function writeCsv(filename: string, rows: string[][]): void {
  {
    const csv = rows.map((row) => row.map(escapeCsvCell).join(",")).join("\r\n");
    // The BOM makes Excel read this as UTF-8. Without it a shed name with an accent, or any
    // non-ASCII operator name, opens as mojibake — the single most common way a correct export
    // still reaches someone unreadable.
    const blob = new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    // Revoking on the next tick rather than immediately: Safari has not always started the download
    // by the time click() returns, and revoking too early cancels it.
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }
}

/**
 * Quotes a CSV cell per RFC 4180, and defuses spreadsheet formula injection.
 *
 * A cell beginning =, +, - or @ is executed as a FORMULA by Excel and Sheets when the file is
 * opened. None of this export's values should ever start that way, but they are farm-authored
 * strings (shed names, operator names, a producer's subject line), so the export must not be the
 * thing that trusts them. Prefixing a tab neutralises the formula while displaying the original
 * text.
 */
function escapeCsvCell(value: string): string {
  const raw = value ?? "";
  const guarded = /^[=+\-@]/.test(raw) ? `\t${raw}` : raw;
  return /[",\r\n\t]/.test(guarded) ? `"${guarded.replace(/"/g, '""')}"` : guarded;
}
