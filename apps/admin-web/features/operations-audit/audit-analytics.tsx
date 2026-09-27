"use client";

import Card from "@mui/material/Card";
import Stack from "@mui/material/Stack";
import Divider from "@mui/material/Divider";
import ButtonBase from "@mui/material/ButtonBase";

import Link from "@/components/no-prefetch-link";
import { Scrollbar } from "@/components/minimal/scrollbar";
import type { IconifyName } from "@/components/minimal/iconify";
import { InvoiceAnalytic } from "@/components/app/sections/invoice/invoice-analytic";

/** Theme palette key for the ring + icon (resolved as `<key>.main`). */
export type InvoiceAnalyticColor = "primary" | "secondary" | "info" | "success" | "warning" | "error";

export type AuditAnalyticCell = {
  key: string;
  title: string;
  /** The hint line under the title (backend copy). */
  total: string;
  price: string;
  percent: number;
  icon: IconifyName;
  color: InvoiceAnalyticColor;
  /** Each cell is a filter shortcut (clear / awaiting / proof gaps / anomalies). */
  href?: string;
};

/**
 * The template invoice list view's analytics Card (sections/invoice/view/invoice-list-view.tsx):
 * InvoiceAnalytic cells split by dashed vertical dividers in a Scrollbar. Each cell keeps the old KPI
 * card's shortcut link. Client leaf: InvoiceAnalytic styles its rings with theme callbacks.
 */
export function AuditAnalytics({ cells }: { cells: AuditAnalyticCell[] }) {
  return (
    <Card>
      <Scrollbar>
        <Stack direction="row" divider={<Divider orientation="vertical" flexItem sx={{ borderStyle: "dashed" }} />} sx={{ py: 2 }}>
          {cells.map((cell) => {
            const analytic = <InvoiceAnalytic title={cell.title} total={typeof cell.price === "number" ? cell.price : 0} caption={cell.total} value={cell.price} percent={cell.percent} icon={cell.icon} color={`${cell.color}.main`} />;
            return cell.href ? (
              <ButtonBase
                key={cell.key}
                component={Link}
                href={cell.href}
                replace
                scroll={false}
                sx={{ flex: 1, minWidth: 200, px: 1, borderRadius: "var(--r-md)", textAlign: "left" }}
              >
                {analytic}
              </ButtonBase>
            ) : (
              <Stack key={cell.key} sx={{ flex: 1 }}>
                {analytic}
              </Stack>
            );
          })}
        </Stack>
      </Scrollbar>
    </Card>
  );
}
