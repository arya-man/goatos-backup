"use client";

import type { ReactNode } from "react";
import Box from "@mui/material/Box";
import Paper from "@mui/material/Paper";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";

import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";

/**
 * Template building blocks shared by the sheet drawer and the workbook drawer (the bulk
 * download / upload surfaces of /configuration/items): a titled section (template drawer
 * section rhythm: subtitle + icon, caption hints, divider between sections), the job card
 * (outlined Paper), the counts grid (overline label over an h6 value), the status Label and the
 * recent-uploads list. Presentation only; every word arrives from the page contract.
 */

export function SheetDrawerBody({ children, testId }: { children: ReactNode; testId: string }) {
  return (
    <Stack spacing={2.25} data-testid={testId} sx={{ minWidth: 0 }}>
      {children}
    </Stack>
  );
}

export function SheetSection({ icon, title, children }: { icon: IconifyName; title: ReactNode; children: ReactNode }) {
  return (
    <Stack
      component="section"
      spacing={1}
      sx={{ pb: 1.75, borderBottom: (theme) => `1px solid ${theme.vars.palette.divider}`, "&:last-of-type": { borderBottom: 0, pb: 0 } }}
    >
      <Stack direction="row" spacing={0.75} sx={{ alignItems: "center" }}>
        <Iconify icon={icon} width={18} aria-hidden="true" />
        <Typography variant="subtitle2">{title}</Typography>
      </Stack>
      {children}
    </Stack>
  );
}

export function SheetHint({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <Typography variant="body2" data-testid={testId} sx={{ color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

export function SheetActions({ children }: { children: ReactNode }) {
  return <Stack direction="row" sx={{ flexWrap: "wrap", gap: 1 }}>{children}</Stack>;
}

export function SheetSubtitle({ children }: { children: ReactNode }) {
  return (
    <Typography variant="overline" sx={{ color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

export function SheetStatus({ status, label }: { status: string; label: string }) {
  return (
    <Label variant="soft" color={status === "applied" ? "success" : status === "failed" ? "error" : "default"}>
      {label}
    </Label>
  );
}

export function SheetJobCard({ children, testId, status, fileName, statusLabel }: { children: ReactNode; testId: string; status: string; fileName: string; statusLabel: string }) {
  return (
    <Paper variant="outlined" data-testid={testId} data-status={status} sx={{ p: 1.5, borderRadius: 1, display: "flex", flexDirection: "column", gap: 1 }}>
      <Stack direction="row" sx={{ flexWrap: "wrap", alignItems: "center", justifyContent: "space-between", gap: 1, minWidth: 0 }}>
        <Typography variant="subtitle2" sx={{ minWidth: 0, maxWidth: 1, overflowWrap: "anywhere" }}>
          {fileName}
        </Typography>
        <SheetStatus status={status} label={statusLabel} />
      </Stack>
      {children}
    </Paper>
  );
}

export function SheetCounts({ items }: { items: { label: string; value: ReactNode; testId: string }[] }) {
  return (
    <Box component="dl" sx={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(90px, 1fr))", gap: 0.75, m: 0 }}>
      {items.map((item) => (
        <Box key={item.testId} sx={{ display: "flex", flexDirection: "column" }}>
          <Typography component="dt" variant="caption" sx={{ color: "text.secondary" }}>
            {item.label}
          </Typography>
          <Typography component="dd" variant="h6" data-testid={item.testId} sx={{ m: 0 }}>
            {item.value}
          </Typography>
        </Box>
      ))}
    </Box>
  );
}

export function SheetRecent({ title, items }: { title: string; items: { id: string; fileName: string; meta: ReactNode }[] }) {
  return (
    <Stack spacing={0.5}>
      <SheetSubtitle>{title}</SheetSubtitle>
      <Box component="ul" sx={{ m: 0, p: 0, listStyle: "none", display: "flex", flexDirection: "column", gap: 0.5 }}>
        {items.map((item) => (
          <Box component="li" key={item.id} sx={{ display: "flex", flexWrap: "wrap", justifyContent: "space-between", gap: 1, minWidth: 0 }}>
            <Typography variant="body2" sx={{ minWidth: 0, maxWidth: 1, overflowWrap: "anywhere" }}>
              {item.fileName}
            </Typography>
            <Typography variant="caption" sx={{ color: "text.secondary", flex: "0 0 auto" }}>
              {item.meta}
            </Typography>
          </Box>
        ))}
      </Box>
    </Stack>
  );
}
