"use client";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import Typography from "@mui/material/Typography";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Caption } from "@/components/app/caption";
import { fmtDate } from "@/lib/format";
import type { FeedAnalyticsFollowUpResponse, FeedAnalyticsFollowUpRow } from "@/lib/api/server";
import { EmptyState } from "@/components/app/empty-state";
import { TableHeadCustom } from "@/components/app/table";
import { labelClasses } from "@/components/minimal/label";

// ---------------------------------------------------------------------------
// Feed follow-up: when animals were bought, sold or lost, did the feed change?
//
// ONE FLAT TABLE, one row per change (maintainer instruction 2026-09-22). It
// had a verdict strip, a summary of the whole range and a pen row that expanded
// into its days -- three layers to get through before reaching the only two
// facts anyone wanted: when it happened, and what the feed did. Those are
// columns on one line now.
//
// The grain is the CHECK, not the pen: one line per (pen, the day the feed was
// supposed to change). A pen that lost animals three times is three lines,
// which is what a reader scanning for "did this get handled" actually wants.
//
// Every number and every word is the backend's (AGENTS.md golden frontend
// rule). This file owns the LOOK and computes no business figure of its own --
// in particular it does NOT re-derive a verdict: `status` arrives per check and
// a client that recomputed it would be a second opinion able to disagree with
// the server's on the same line.
// ---------------------------------------------------------------------------

type Row = FeedAnalyticsFollowUpRow;
type Check = Row["days"][number];
/** One line of the table: the pen it happened in, and the check itself. */
type Line = { pen: Row; check: Check };

const num = (raw: string) => {
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : 0;
};
const kg = (raw: string) => (raw === "" ? "—" : Number(num(raw).toFixed(1)).toLocaleString("en-IN"));

function fa(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, key);
}

/**
 * Newest change first, by the day the animals moved (maintainer request 2026-09-24: "sort using
 * dates"). Ranking the unfollowed lines first put a 29/08 line above everything from September.
 * Within a day, farm then pen, pens compared as numbers (Part 2 before Part 10).
 */
function linesOf(data: FeedAnalyticsFollowUpResponse): Line[] {
  const lines: Line[] = [];
  for (const pen of data.rows) {
    for (const check of pen.days) lines.push({ pen, check });
  }
  return lines.sort((a, b) => {
    if (a.check.event_date !== b.check.event_date) {
      return a.check.event_date < b.check.event_date ? 1 : -1;
    }
    return (
      a.pen.park_label.localeCompare(b.pen.park_label) ||
      a.pen.operational_location_display.localeCompare(b.pen.operational_location_display, undefined, { numeric: true })
    );
  });
}

export function FeedFollowUpTab({
  data,
  pageContract,
  page,
}: {
  data: FeedAnalyticsFollowUpResponse;
  pageContract: AdminUiPageContract;
  /** URL paging over the date-sorted lines (ffu_offset / ffu_limit); the pager is drawn by the page. */
  page: { offset: number; limit: number };
}) {
  const allLines = linesOf(data);
  // Sorted over the WHOLE window first, then sliced, so page 2 continues page 1's dates.
  const lines = allLines.slice(page.offset, page.offset + page.limit);
  return (
    <Card component="section">
      <CardHeader title={fa(pageContract, "followup.table.title")} subheader={<Caption>{fa(pageContract, "followup.hint")}</Caption>} sx={{ mb: 2 }} />
      {allLines.length === 0 ? (
        <Box sx={{ px: 3, pb: 3 }}>
          <EmptyState title={fa(pageContract, "followup.empty")} />
        </Box>
      ) : (
        <Box sx={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={fa(pageContract, "followup.table.title")}>
          <Table sx={{ "& td": { verticalAlign: "top" } }}>
            <TableHeadCustom
              headCells={[
                { id: "pen", label: fa(pageContract, "followup.col.pen") },
                { id: "changes", label: fa(pageContract, "followup.col.changes") },
                { id: "when", label: fa(pageContract, "followup.col.when") },
                { id: "animals", label: fa(pageContract, "followup.col.animals"), align: "right" },
                { id: "feed", label: fa(pageContract, "followup.col.feed"), align: "right" },
                { id: "status", label: fa(pageContract, "followup.col.status") },
              ]}
            />
            <TableBody>
              {lines.map(({ pen, check }) => (
                <FollowUpLine
                  key={`${pen.park_id}:${pen.shed_id}:${pen.partition_label}:${check.event_date}`}
                  pen={pen}
                  check={check}
                  pageContract={pageContract}
                />
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
    </Card>
  );
}

function FollowUpLine({
  pen,
  check,
  pageContract,
}: {
  pen: Row;
  check: Check;
  pageContract: AdminUiPageContract;
}) {
  const tone = check.status === "not_followed" ? "dng" : check.status === "pending" ? "warn" : "ok";
  const pending = check.after_day === "";
  return (
    <TableRow>
      <TableCell>
        {/* Backend-composed pen name, rendered verbatim (operational-location convention). */}
        <Typography component="span" variant="subtitle2">
          {pen.operational_location_display}
        </Typography>
        <Typography component="span" variant="body2" sx={{ color: "text.secondary" }}>
          {" "}· {pen.park_label}
        </Typography>
      </TableCell>
      <TableCell>
        <Box component="span" sx={{ display: "inline-flex", flexWrap: "wrap", gap: 0.625 }}>
          {check.events.map((event) => (
            <Tag
              key={`${event.kind}-${event.event_date}`}
              tone={event.kind === "purchased" ? "ok" : event.kind === "sold" ? "info" : "dng"}
              // The animals themselves, on hover: RFID / tag values, never goat
              // ids. They do not need a line of their own to be reachable.
              title={event.tags.join(", ")}
            >
              {fa(pageContract, `followup.cause.${event.kind}`)} {event.animals}
            </Tag>
          ))}
        </Box>
        {/* The day the animals moved. Only the DAY -- the recorded time of day
            is a batch data-entry stamp, so printing it would invite a reader to
            reason from a clock that means nothing. */}
        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {fmtDate(check.event_date)}
        </Typography>
      </TableCell>
      <TableCell sx={{ color: "text.secondary", typography: "body2" }}>
        {/* The two sheets compared. Naming them is what keeps a negative
            verdict an observation rather than an accusation.

            Both days go through fmtDate like every other date on the page --
            they arrive as the wire's ISO business day (`feed_day::text`) and
            were being substituted into the contract's copy verbatim, so this
            column read `2026-09-21 -> 2026-09-23` beside the event date one
            cell to its left already reading `21/09/2026`. */}
        {pending
          ? fa(pageContract, "followup.day.pending")
          : fa(pageContract, "followup.day.sheets")
              .replace("{before}", fmtDate(check.before_day))
              .replace("{after}", fmtDate(check.after_day))}
      </TableCell>
      <TableCell align="right">
        <Movement
          before={String(check.head_before)}
          after={pending ? "—" : String(check.head_after)}
        />
      </TableCell>
      <TableCell align="right">
        <Movement before={kg(check.kg_before)} after={pending ? "—" : kg(check.kg_after)} />
      </TableCell>
      {/* The result and its footnote STACK. Side by side they pushed the
          column past the table's edge and the chip rendered clipped. */}
      <TableCell sx={{ whiteSpace: "nowrap", [`& > .${labelClasses.root} + .${labelClasses.root}`]: { display: "flex", width: "fit-content", mt: 0.5 } }}>
        <Tag tone={tone}>{fa(pageContract, `followup.status.${check.status}`)}</Tag>
        {/* Only where the feed DID move by an amount the causes do not explain
            -- usually animals shifted in or out. On a line where nothing moved,
            the unexplained figure is always the net of the causes already shown
            in the column beside it, so printing it again is noise. */}
        {check.status === "followed" && check.unexplained !== 0 ? (
          <Tag tone="mut" title={fa(pageContract, "followup.unexplained.help")}>
            {fa(pageContract, "followup.unexplained.chip").replace(
              "{count}",
              check.unexplained > 0 ? `+${check.unexplained}` : String(check.unexplained),
            )}
          </Tag>
        ) : null}
      </TableCell>
    </TableRow>
  );
}

/** "53 → 48", the whole point of the two number columns. */
function Movement({ before, after }: { before: string; after: string }) {
  const changed = before !== after;
  return (
    <Box component="span" sx={{ display: "inline-flex", alignItems: "baseline", justifyContent: "flex-end", gap: 0.75, fontVariantNumeric: "tabular-nums" }}>
      <Box component="span" sx={{ color: "text.secondary" }}>
        {before}
      </Box>
      <Box component="span" aria-hidden="true" sx={{ color: "text.secondary", typography: "caption" }}>
        →
      </Box>
      <Box component="span" sx={{ fontWeight: changed ? "fontWeightBold" : undefined }}>
        {after}
      </Box>
    </Box>
  );
}
