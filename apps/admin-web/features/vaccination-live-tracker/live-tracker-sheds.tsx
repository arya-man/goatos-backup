import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import MuiLink from "@mui/material/Link";
import { PagedRows } from "@/components/app/paged-rows";
import Link from "@/components/no-prefetch-link";
import { ClipText } from "@/components/ui-primitives";
import {
  copy,
  optionGroup,
  optionLabel,
  optionTitle,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import type { LiveTrackerShedRow } from "@/lib/api/vaccination-live-tracker";
import { fmtClock, paletteOf, pct, progressColor, progressTone } from "./format";
import { LiveDot, LiveEmpty, LiveHeadRow, LiveNote, LiveProgress } from "./live-ui";
import { LiveStateTag } from "./live-state-tag";

// Sheds — proof progress. Grain is shed × partition × vaccine × operator, which is what the mock's
// rows actually are ("Gandhi 2 / Goat Pox / Kumar Sharath").
//
// The legend renders the FULL state vocabulary from the contract, including the fifth state the
// mock's own legend omitted while its rows displayed it.
export function LiveTrackerSheds({
  rows,
  total,
  truncated,
  hasFilter,
  resetHref,
  shedHref,
  pageContract,
}: {
  rows: LiveTrackerShedRow[];
  total: number;
  truncated: boolean;
  hasFilter: boolean;
  resetHref: string;
  shedHref: (row: LiveTrackerShedRow) => string;
  pageContract: AdminUiPageContract;
}) {
  const cols = tableLabels(pageContract, "live-sheds");
  const legend = optionGroup(pageContract, "live_shed_state");

  const shedRows = rows.map((row) => {
    // Closure is what Remaining and the status pill are derived from, so it is what the bar shows.
    // Proof arrival stays in its own column beside it.
    const tone = progressTone(row.state);
    const percent = pct(row.closed_administrations, row.scheduled_administrations);
    const href = shedHref(row);
    return (
      <TableRow hover key={`${row.shed_id}|${row.partition_label}|${row.vaccine_code}|${row.operator_id}`}>
        <TableCell sx={{ maxWidth: 180 }}>
          <MuiLink
            component={Link}
            href={href}
            scroll={false}
            color="inherit"
            underline="hover"
            aria-label={`${copy(pageContract, "section.sheds.title")} — ${row.shed_label}`}
            sx={{ typography: "subtitle2" }}
          >
            <ClipText title={row.shed_label}>{row.shed_label}</ClipText>
          </MuiLink>
        </TableCell>
        <TableCell>{row.vaccine_label || copy(pageContract, "label.placeholder")}</TableCell>
        <TableCell sx={{ maxWidth: 160 }}>
          <ClipText title={row.operator_name}>{row.operator_name || copy(pageContract, "label.placeholder")}</ClipText>
        </TableCell>
        <TableCell align="right">{row.scheduled_administrations}</TableCell>
        <TableCell align="right">{row.proof_videos_received}</TableCell>
        <TableCell align="right">{row.closed_administrations}</TableCell>
        <TableCell align="right">{row.remaining}</TableCell>
        <TableCell sx={{ minWidth: 150 }}>
          <LiveProgress value={percent} color={progressColor(tone)} label={`${percent}%`} />
        </TableCell>
        <TableCell sx={{ color: "text.secondary", whiteSpace: "nowrap" }}>{fmtClock(row.last_proof_at) || copy(pageContract, "label.placeholder")}</TableCell>
        <TableCell>
          <LiveStateTag
            pageContract={pageContract}
            group="live_shed_state"
            stateKey={row.state}
            title={optionTitle(pageContract, "live_shed_state", row.state)}
          >
            {/* The count only leads the label when there is more than one. "1 extra attempts" is the
                kind of small wrongness that makes a reader distrust every other number. */}
            {row.state === "review" && row.extra_attempt_count > 1
              ? `${row.extra_attempt_count} ${optionLabel(pageContract, "live_shed_state", "review")}`
              : optionLabel(pageContract, "live_shed_state", row.state)}
          </LiveStateTag>
        </TableCell>
      </TableRow>
    );
  });

  // Template table card (order list anatomy): CardHeader with the state legend as its action,
  // Scrollbar + head row, 10 rows a page with the template pager.
  return (
    <Card id="lt-sheds" sx={{ scrollMarginTop: 80 }}>
      <CardHeader
        title={copy(pageContract, "section.sheds.title")}
        action={
          <Box component="ul" aria-label={copy(pageContract, "legend.label")} sx={{ m: 0, p: 0, listStyle: "none", display: "flex", flexWrap: "wrap", columnGap: 2, rowGap: 0.5, justifyContent: "flex-end" }}>
            {legend.map((option) => (
              <Box component="li" key={option.key} sx={{ display: "flex", alignItems: "center", gap: 0.75, typography: "caption", color: "text.secondary" }}>
                <LiveDot color={paletteOf(option.tone || "mut")} />
                {option.label}
              </Box>
            ))}
          </Box>
        }
        slotProps={{ action: { sx: { alignSelf: "center", maxWidth: { md: "55%" } } } }}
        sx={{ mb: 2, flexWrap: { xs: "wrap", md: "nowrap" }, rowGap: 1 }}
      />

      {rows.length === 0 ? (
        <LiveEmpty
          title={hasFilter ? copy(pageContract, "section.sheds.filtered_title") : copy(pageContract, "section.sheds.empty_title")}
          body={hasFilter ? copy(pageContract, "section.sheds.filtered_body") : copy(pageContract, "section.sheds.empty_body")}
          resetHref={hasFilter ? resetHref : null}
          resetLabel={copy(pageContract, "action.reset_filters")}
        />
      ) : (
        <>
          <PagedRows
            scrollbar
            tableMinWidth={960}
            ariaLabel={copy(pageContract, "section.sheds.title")}
            head={<LiveHeadRow labels={cols} numeric={[3, 4, 5, 6]} />}
            rows={shedRows}
          />
          {/* The shed board is capped server-side. The tiles above are folded from the untruncated
              rollup, so past the cap they legitimately exceed this table's Scheduled column — and a
              reader can only reconcile that if the page says the table is partial. */}
          {truncated ? (
            <LiveNote>
              <b>
                {rows.length}/{total}
              </b>{" "}
              {copy(pageContract, "section.sheds.truncated_note")}
            </LiveNote>
          ) : null}
        </>
      )}
    </Card>
  );
}
