import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Button from "@mui/material/Button";
// The Approvals queue card, split out of approvals-page.tsx so it renders without the server
// reads: the page passes the fetched rows in, and Storybook renders the same markup from a fixture
// (stories/approvals-queue.stories.tsx) to pin the 390px card layout. Presentation only.
//
// Template anatomy: sections/order/view/order-list-view.tsx — one Card holding the Tabs row, the
// toolbar row (OrderTableToolbar: p 2.5, gap 2), the Scrollbar'd table with TableHeadCustom, the
// OrderTableRow cells (Label for type/status, muted secondary lines) and the pagination footer.
import type { ReactNode } from "react";

import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/minimal/table";
import { EmptyContent } from "@/components/minimal/empty-content";
import type { AdminWebApprovalItem } from "@/lib/api/server";
import { fmtDateTime } from "@/lib/format";
import type { RouteSearchParams } from "@/lib/search-params";
import { approvalStatusLabel } from "./approval-display";
import { APPROVALS_COPY as COPY } from "./copy";

const PATHNAME = "/approvals";

const HEAD = COPY.table.columns.map((label, index) => ({ id: `c${index}`, label, sortable: false, align: index === COPY.table.columns.length - 1 ? ("right" as const) : undefined }));

// Phone (xs): each request is one stacked row — type and status share the top line, the subject
// reads in full, then who raised it and when, then a full-width 44px Review (the 6-column table was
// clipped at 390px). Template-free CSS never re-enters: the stack is sx on the MUI parts.
const ROW_SX = {
  display: { xs: "grid", sm: "table-row" },
  gridTemplateColumns: "minmax(0,1fr) auto",
  gridTemplateAreas: { xs: '"type status" "subject subject" "by by" "raised raised" "action action"' },
  gap: { xs: 0.75 },
  px: { xs: 2.5, sm: 0 },
  py: { xs: 2, sm: 0 },
  borderBottom: { xs: "1px dashed", sm: "none" },
  borderColor: { xs: "divider" },
  "& > td": { display: { xs: "block", sm: "table-cell" }, border: { xs: 0 }, p: { xs: 0 }, minWidth: 0, overflowWrap: "anywhere" },
} as const;

const STATUS_COLOR: Record<string, LabelColor> = { pending: "warning", approved: "success", rejected: "error", cancelled: "default" };

export function ApprovalsQueueTable({
  items,
  ok,
  searchParams,
  subjects,
  tabs,
  toolbar,
  footer,
}: {
  items: AdminWebApprovalItem[];
  ok: boolean;
  searchParams: RouteSearchParams;
  /** Readable subject per approval_request_id, composed by the page through approvalSubject. */
  subjects: Record<string, string>;
  /** Template list Tabs row (request type). */
  tabs?: ReactNode;
  /** Template toolbar row (status, farm, date). */
  toolbar?: ReactNode;
  /** Pager row under the table. */
  footer?: ReactNode;
}) {
  return (
    <Card sx={{ minWidth: 0 }} data-testid="approvals-queue">
      {tabs}
      {toolbar}
      <Scrollbar sx={{ minHeight: 0 }}>
        <Table aria-label={COPY.title} sx={{ minWidth: { sm: 860 }, display: { xs: "block", sm: "table" }, "& > tbody": { display: { xs: "block", sm: "table-row-group" } } }}>
          <TableHeadCustom headCells={HEAD} sx={{ display: { xs: "none", sm: "table-header-group" } }} />
          <TableBody>
            {items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={HEAD.length}>
                  <EmptyContent
                    filled
                    title={ok ? COPY.error.empty : COPY.error.queueUnavailable}
                    sx={{ py: 10 }}
                    slotProps={{ img: { alt: "" } }}
                    role="status"
                  />
                </TableCell>
              </TableRow>
            ) : (
              items.map((item) => (
                <ApprovalRow key={item.approval_request_id} item={item} searchParams={searchParams} subject={subjects[item.approval_request_id] ?? ""} />
              ))
            )}
          </TableBody>
        </Table>
      </Scrollbar>
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
    <TableRow hover sx={ROW_SX}>
      <TableCell data-label={COPY.table.columns[0]} sx={{ gridArea: "type" }}>
        <Label variant="soft" color={item.request_type === "shifting" ? "info" : "warning"}>
          {titleCaseType(item.request_type)}
        </Label>
      </TableCell>
      <TableCell data-label={COPY.table.columns[1]} sx={{ gridArea: "subject", typography: "subtitle2" }}>
        {subject}
      </TableCell>
      <TableCell data-label={COPY.table.columns[2]} sx={{ gridArea: "by", color: "text.secondary", "&:empty": { display: { xs: "none", sm: "table-cell" } } }}>
        {item.raised_by_name ?? ""}
      </TableCell>
      <TableCell data-label={COPY.table.columns[3]} sx={{ gridArea: "raised", color: "text.secondary", whiteSpace: { sm: "nowrap" } }}>
        {fmtDateTime(item.raised_at)}
      </TableCell>
      <TableCell data-label={COPY.table.columns[4]} sx={{ gridArea: "status", justifySelf: "end" }}>
        <Label variant="soft" color={STATUS_COLOR[item.status] ?? "default"}>
          {approvalStatusLabel(item.status)}
        </Label>
      </TableCell>
      <TableCell data-label={COPY.table.columns[5]} align="right" sx={{ gridArea: "action", mt: { xs: 0.75, sm: 0 } }}>
        <Box sx={{ display: "flex", justifyContent: { xs: "stretch", sm: "flex-end" } }}>
          <Button
            component={LocalOverlayLink}
            href={href}
            scroll={false}
            variant="outlined"
            color="inherit"
            size="small"
            sx={{ width: { xs: 1, sm: "auto" }, minHeight: { xs: 44, sm: 30 } }}
          >
            Review
          </Button>
        </Box>
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
