"use client";

import Card from "@mui/material/Card";
import Stack from "@mui/material/Stack";
import Divider from "@mui/material/Divider";

import Box from "@mui/material/Box";
import type { IconifyName } from "@/components/minimal/iconify";
import { InvoiceAnalytic } from "@/components/app/sections/invoice/invoice-analytic";

/** Theme palette key for the ring + icon (resolved as `<key>.main`). */
export type InvoiceAnalyticColor = "primary" | "secondary" | "info" | "success" | "warning" | "error";

export type DLQAnalyticCell = { key: string; title: string; total: string; price: number; percent: number; icon: IconifyName; color: InvoiceAnalyticColor };

/**
 * The template invoice list view's analytics Card (sections/invoice/view/invoice-list-view.tsx):
 * a Scrollbar row of InvoiceAnalytic cells split by dashed vertical dividers. A client leaf because
 * InvoiceAnalytic styles its rings with theme callbacks, which a server page cannot hand to MUI.
 */
export function DLQAnalytics({ cells }: { cells: DLQAnalyticCell[] }) {
  return (
    <Card>
      {/* Plain overflow box, not the SimpleBar Scrollbar: this strip is re-keyed on every tab click
          and SimpleBar is 0px tall for its first frame, which moved the tabs below 88px.
          guard: audit-strip-no-simplebar */}
      <Box sx={{ overflowX: "auto", overscrollBehaviorX: "contain" }}>
        <Stack direction="row" divider={<Divider orientation="vertical" flexItem sx={{ borderStyle: "dashed" }} />} sx={{ py: 2 }}>
          {cells.map((cell) => (
            <InvoiceAnalytic key={cell.key} title={cell.title} total={typeof cell.price === "number" ? cell.price : 0} caption={cell.total || undefined} value={cell.price} percent={cell.percent} icon={cell.icon} color={`${cell.color}.main`} />
          ))}
        </Stack>
      </Box>
    </Card>
  );
}
