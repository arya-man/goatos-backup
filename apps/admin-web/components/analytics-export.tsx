"use client";

import { Iconify } from "@/components/minimal/iconify";

import Button from "@mui/material/Button";

/**
 * The Feed Analytics page's primary action: take the window currently on screen away as a CSV.
 *
 * It exports the ROWS THE SERVER ALREADY SENT for this request — the same figures the charts above
 * are drawing — rather than issuing a second read. That is deliberate: a fresh fetch could answer
 * from a different window (the date arithmetic is IST, and this page is routinely open across
 * midnight), and an export that disagrees with the chart beside it is worse than no export.
 *
 * No business arithmetic here: `rows` arrive fully formed from the server component.
 */
export function FeedAnalyticsExport({
  rows,
  filename,
  label,
  disabled,
}: {
  rows: readonly (readonly (string | number)[])[];
  filename: string;
  label: string;
  disabled?: boolean;
}) {
  function cell(value: string | number): string {
    const text = String(value ?? "");
    return /[",\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
  }
  return (
    <Button
      variant="contained"
      color="primary"
      disabled={disabled || rows.length === 0}
      startIcon={<Iconify icon="solar:download-bold" width={16} />}
      onClick={() => {
        const body = `${rows.map((row) => row.map(cell).join(",")).join("\n")}\n`;
        const url = URL.createObjectURL(new Blob([body], { type: "text/csv" }));
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = filename.toLowerCase().endsWith(".csv") ? filename : `${filename}.csv`;
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
        URL.revokeObjectURL(url);
      }}
    >
      {label}
    </Button>
  );
}
