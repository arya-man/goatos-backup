import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import ListItemText from "@mui/material/ListItemText";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { PagedRows } from "@/components/app/paged-rows";
import { Label } from "@/components/minimal/label";
import { ClipText } from "@/components/ui-primitives";
import {
  copy,
  optionLabel,
  optionTitle,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import type { LiveTrackerOperatorRow } from "@/lib/api/vaccination-live-tracker";
import { fmtClock, initials, pct, progressColor, progressTone } from "./format";
import { LiveEmpty, LiveHeadRow, LiveNote, LiveProgress } from "./live-ui";
import { LiveStateTag } from "./live-state-tag";

// Operators — live. One row per operator on the drive day, at ADMINISTRATION grain.
//
// The sum of the Scheduled column equals the Scheduled tile MINUS unassignedAdmins: work in a
// shed/partition with no drive assignment for the day has no operator to be attributed to, so it is
// counted in the tiles and listed under Sheds but appears in no row here. That residual is rendered
// as its own note rather than left as an unexplained gap between a tile and the table under it — on
// a partially-planned stg day it is 95 of 199.
export function LiveTrackerOperators({
  rows,
  parkCount,
  total,
  truncated,
  unassignedAdmins,
  hasFilter,
  resetHref,
  pageContract,
}: {
  rows: LiveTrackerOperatorRow[];
  parkCount: number;
  total: number;
  truncated: boolean;
  unassignedAdmins: number;
  hasFilter: boolean;
  resetHref: string;
  pageContract: AdminUiPageContract;
}) {
  const cols = tableLabels(pageContract, "live-operators");

  const countLabel = `${rows.length} ${
    rows.length === 1 ? copy(pageContract, "section.operators.count_suffix_one") : copy(pageContract, "section.operators.count_suffix")
  } · ${parkCount} ${parkCount === 1 ? copy(pageContract, "section.operators.park_suffix_one") : copy(pageContract, "section.operators.park_suffix")}`;

  const operatorRows = rows.map((row) => {
    const done = row.closed_administrations;
    const tone = progressTone(row.state);
    const stateKey = row.state;
    const subLine = row.current_vaccine_label
      ? row.current_vaccine_label
      : row.last_activity_at
        ? `${copy(pageContract, "section.operators.now_at_prefix")} ${fmtClock(row.last_activity_at)}`
        : optionLabel(pageContract, "live_operator_state", "not_started");
    const stateLabel =
      stateKey === "idle" && row.idle_minutes != null
        ? `${copy(pageContract, "section.operators.idle_prefix")} ${row.idle_minutes} ${copy(pageContract, "section.operators.idle_suffix")}`
        : optionLabel(pageContract, "live_operator_state", stateKey);
    return (
      <TableRow hover key={row.operator_id}>
        {/* Template user-list name cell: avatar + name. */}
        <TableCell>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, minWidth: 0 }}>
            <Avatar aria-hidden="true" sx={{ width: "var(--sp-4)", height: "var(--sp-4)", typography: "caption", fontWeight: "fontWeightBold" }}>
              {initials(row.operator_name)}
            </Avatar>
            <Box sx={{ minWidth: 0, maxWidth: 160, typography: "subtitle2" }}>
              <ClipText title={row.operator_name}>{row.operator_name}</ClipText>
            </Box>
          </Box>
        </TableCell>
        <TableCell>{row.park_code || row.park_name || copy(pageContract, "label.placeholder")}</TableCell>
        <TableCell>
          <ListItemText
            primary={row.current_shed_label || copy(pageContract, "label.placeholder")}
            secondary={subLine}
            slotProps={{ primary: { sx: { typography: "body2" } }, secondary: { sx: { typography: "caption", color: "text.disabled" } } }}
            sx={{ m: 0 }}
          />
        </TableCell>
        <TableCell align="right">{row.scheduled_administrations}</TableCell>
        <TableCell align="right">{row.proof_videos}</TableCell>
        <TableCell align="right">{row.scan_captures}</TableCell>
        <TableCell align="right">{row.closed_administrations}</TableCell>
        <TableCell align="right">{row.remaining}</TableCell>
        <TableCell sx={{ minWidth: 150 }}>
          <LiveProgress value={pct(done, row.scheduled_administrations)} color={progressColor(tone)} label={`${done}/${row.scheduled_administrations}`} />
        </TableCell>
        <TableCell>
          <LiveStateTag
            pageContract={pageContract}
            group="live_operator_state"
            stateKey={stateKey}
            title={optionTitle(pageContract, "live_operator_state", stateKey)}
          >
            {stateLabel}
          </LiveStateTag>
        </TableCell>
      </TableRow>
    );
  });

  // Template table card: CardHeader (count Label beside the title, drill-down note as subheader).
  return (
    <Card id="lt-operators" sx={{ scrollMarginTop: 80 }}>
      <CardHeader
        title={
          <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
            {copy(pageContract, "section.operators.title")}
            <Label variant="soft">{countLabel}</Label>
          </Box>
        }
        subheader={copy(pageContract, "section.operators.drilldown_note")}
        sx={{ mb: 2 }}
      />

      {rows.length === 0 ? (
        <LiveEmpty
          title={hasFilter ? copy(pageContract, "section.operators.filtered_title") : copy(pageContract, "section.operators.empty_title")}
          body={hasFilter ? copy(pageContract, "section.operators.filtered_body") : copy(pageContract, "section.operators.empty_body")}
          resetHref={hasFilter ? resetHref : null}
          resetLabel={copy(pageContract, "action.reset_filters")}
        />
      ) : (
        <>
          <PagedRows
            scrollbar
            tableMinWidth={1040}
            ariaLabel={copy(pageContract, "section.operators.title")}
            head={<LiveHeadRow labels={cols} numeric={[3, 4, 5, 6, 7]} />}
            rows={operatorRows}
            initialRowsPerPage={5}
            rowsPerPageOptions={[5, 10, 25]}
          />
          {/* Rows may never vanish silently. The KPI tiles are folded from the untruncated rollup,
              so past the cap the Scheduled tile legitimately reads higher than this table sums —
              which is unreadable unless the page says why. */}
          {truncated ? (
            <LiveNote>
              <b>
                {rows.length}/{total}
              </b>{" "}
              {copy(pageContract, "section.operators.truncated_note")}
            </LiveNote>
          ) : null}
          {/* The residual is stated, never left implicit. Without it the Scheduled column simply
              sums short of the tile above with nothing on screen to explain the difference. */}
          {unassignedAdmins > 0 ? (
            <LiveNote>
              <b>{unassignedAdmins}</b> {copy(pageContract, "section.operators.unassigned_note")}
            </LiveNote>
          ) : null}
        </>
      )}
    </Card>
  );
}
