"use client";

import { Download } from "lucide-react";

/**
 * Downloads the Feed Verification day as a CSV -- one row per feed item of every bag in the panel's
 * park scope, exactly the rows on screen.
 *
 * The rows are built on the SERVER and handed in: a feed day is bounded by pens x sessions x items
 * (physical infrastructure, never herd size), so they are already in the payload and a second fetch
 * would buy nothing. Dates and times are formatted there too, in farm time, so the file reads the
 * same wherever it is downloaded.
 */
export function FeedVerificationCsvButton({ filename, label, rows }: { filename: string; label: string; rows: string[][] }) {
  function onClick(): void {
    const csv = rows.map((row) => row.map(escapeCsvCell).join(",")).join("\r\n");
    const blob = new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }

  return (
    <button type="button" className="btn sm vl-csv-btn" onClick={onClick}>
      <Download className="ic" aria-hidden="true" />
      {label}
    </button>
  );
}

// RFC 4180 quoting plus spreadsheet formula-injection defusing (pen and feed names are farm-authored
// strings): a leading = + - @ is prefixed with a tab. Same rule as the Video Log export. A negative
// DIFFERENCE is a number, not a formula, so numeric cells are exempt from the prefix.
function escapeCsvCell(value: string): string {
  const raw = value ?? "";
  const numeric = /^-?\d+(\.\d+)?$/.test(raw);
  const guarded = !numeric && /^[=+\-@]/.test(raw) ? `\t${raw}` : raw;
  return /[",\r\n\t]/.test(guarded) ? `"${guarded.replace(/"/g, '""')}"` : guarded;
}
