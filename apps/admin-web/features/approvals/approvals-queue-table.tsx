import Card from "@mui/material/Card";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
// The Approvals queue table, split out of approvals-page.tsx so it renders without the server
// reads: the page passes the fetched rows in, and Storybook renders the same markup from a fixture
// (stories/approvals-queue.stories.tsx) to pin the 390px card layout. Presentation only.
import type { ReactNode } from "react";
import { Gavel } from "lucide-react";

import { LocalOverlayLink } from "@/components/local-overlay-link";
import { IconBadge } from "@/components/app/icon-badge";
import { QueueEmptyState, StatusChip } from "@/components/review-queue/review-queue-ui";
import { Tag } from "@/components/ui-primitives";
import type { AdminWebApprovalItem } from "@/lib/api/server";
import { fmtDateTime } from "@/lib/format";
import type { RouteSearchParams } from "@/lib/search-params";
import { approvalStatusLabel } from "./approval-display";
import { APPROVALS_COPY as COPY } from "./copy";

const PATHNAME = "/approvals";

export function ApprovalsQueueTable({
  items,
  ok,
  searchParams,
  subjects,
  footer,
}: {
  items: AdminWebApprovalItem[];
  ok: boolean;
  searchParams: RouteSearchParams;
  /** Readable subject per approval_request_id, composed by the page through approvalSubject. */
  subjects: Record<string, string>;
  /** Pager row under the table. */
  footer?: ReactNode;
}) {
  return (
    <Card sx={{ minWidth: 0 }}>
      <div className="hd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "20px 24px" }}>
        <IconBadge icon={<Gavel />} tone="primary" size="sm" />
        <h3 style={{ margin: 0 }}>{COPY.title}</h3>
      </div>
      <div className="bd twrap" style={{ padding: 0 }} tabIndex={0} role="group">
        <Table data-enh="1" className="ap-queue-table">
          <TableHead>
            <TableRow>
              {COPY.table.columns.map((label) => (
                <TableCell component="th" key={label}>{label}</TableCell>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={COPY.table.columns.length}>
                  <QueueEmptyState ok={ok} title={ok ? COPY.error.empty : COPY.error.queueUnavailable} />
                </TableCell>
              </TableRow>
            ) : (
              items.map((item) => (
                <ApprovalRow key={item.approval_request_id} item={item} searchParams={searchParams} subject={subjects[item.approval_request_id] ?? ""} />
              ))
            )}
          </TableBody>
        </Table>
      </div>
      {footer}
    </Card>
  );
}

function ApprovalRow({
  item,
  searchParams,
  subject,
}: {
  item: AdminWebApprovalItem;
  searchParams: RouteSearchParams;
  subject: string;
}) {
  const href = approvalsHref(searchParams, { ap_row: item.approval_request_id, ap_status: null, ap_code: null });
  return (
    <TableRow>
      <TableCell data-label={COPY.table.columns[0]}>
        <Tag tone={item.request_type === "shifting" ? "info" : "warn"}>{titleCaseType(item.request_type)}</Tag>
      </TableCell>
      <TableCell data-label={COPY.table.columns[1]} className="ap-subject">
        {subject}
      </TableCell>
      <TableCell data-label={COPY.table.columns[2]} className="muted">
        {item.raised_by_name ?? ""}
      </TableCell>
      <TableCell data-label={COPY.table.columns[3]} className="muted" style={{ whiteSpace: "nowrap" }}>
        {fmtDateTime(item.raised_at)}
      </TableCell>
      <TableCell data-label={COPY.table.columns[4]}>
        <StatusChip status={item.status}>{approvalStatusLabel(item.status)}</StatusChip>
      </TableCell>
      <TableCell data-label={COPY.table.columns[5]} className="ap-action">
        <LocalOverlayLink href={href} className="btn sm" scroll={false}>
          Review
        </LocalOverlayLink>
      </TableCell>
    </TableRow>
  );
}

export function titleCaseType(v: string): string {
  return v ? v.charAt(0).toUpperCase() + v.slice(1) : v;
}

export function approvalsHref(params: RouteSearchParams, updates: Record<string, string | null | undefined>): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null || value === undefined || value === "") next.delete(key);
    else next.set(key, value);
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}
