"use client";

import { useMemo, useState } from "react";
import Box from "@mui/material/Box";
import IconButton from "@mui/material/IconButton";
import Stack from "@mui/material/Stack";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { copy, type AdminUiPageContract, type AdminUiTableContract } from "@/lib/admin-ui-contract";
import { operationalLocationLabel } from "@/lib/operational-location";
import { stageLabel } from "@/lib/stage-labels";

import { CensusValueEditor, type CensusSlice } from "./census-value-editor";
import {
  allOpen,
  dominantKey,
  penDetailDomId,
  penRowId,
  pointLabel,
  type CountsBreakdownPenRow,
  type CountsBreakdownPoint,
} from "./counts-breakdown-pens";
import type { InlineChoice } from "./inline-cell-editor";
import { ShedTagEditor } from "./shed-tag-editor";
import type { StageOption } from "./shed-stage-actions";
import Button from "@mui/material/Button";

/**
 * The Counts Breakdown pen table: one line per pen carrying its whole head count with the breed,
 * gender and stage composition beside it, and the exact stage x breed x gender rows one click
 * away, opened IN PLACE under the line.
 *
 * Client-side only for two pieces of local UI state — which pens are open, and the page-local
 * sort. The rows, every chip count, the totals and the pager all come from the server render, and
 * nothing here re-sums anything: a pen's chips are the backend's own re-roll of the rows shown
 * when it opens, so the two can never disagree.
 *
 * Opening a pen never navigates and never refetches. The rows are already in the response (a pen
 * holds a handful of combinations), so the drill-down is instant and works with the page's own
 * inline editors exactly as the combination table does.
 */
type GrainRow = CountsBreakdownPenRow["rows"][number];

/** How many composition buckets a pen line names before the "+N". */
const COMPOSITION_SHOWN = 3;

/** "7 kids · 46 adults": the figure in subtitle2, its noun as a secondary caption. */
function Split({ parts }: { parts: readonly (readonly [number, string])[] }) {
  return (
    <Box component="span" sx={{ whiteSpace: "nowrap" }}>
      {parts.map(([value, noun], index) => (
        <Box component="span" key={noun}>
          {index > 0 ? <Box component="span" sx={{ color: "text.disabled" }}> · </Box> : null}
          <Box component="span" sx={{ typography: "subtitle2" }}>{value}</Box>{" "}
          <Box component="span" sx={{ typography: "caption", color: "text.secondary" }}>{noun}</Box>
        </Box>
      ))}
    </Box>
  );
}

// sliceOf names the row the way the correction write matches it. Every field participates in the
// predicate, so this must stay a faithful copy of the row rather than a convenient subset.
function sliceOf(row: GrainRow): CensusSlice {
  return {
    shedId: row.shed_id ?? "",
    partitionLabel: row.partition_label ?? "",
    managementStage: row.management_stage,
    breed: row.breed,
    sex: row.sex,
  };
}

export function CountsBreakdownPensTable({
  contract,
  pageContract,
  pens,
  ariaLabel,
  empty,
  footer,
  noParkLabel,
  noStageLabel,
  noBreedLabel,
  noSexLabel,
  noShedLabel,
  stages,
  stageLabels,
  breeds,
  genders,
  retagEnabled,
  retagDisabledReason,
}: {
  contract: AdminUiTableContract;
  pageContract: AdminUiPageContract;
  pens: CountsBreakdownPenRow[];
  ariaLabel: string;
  empty: React.ReactNode;
  footer?: React.ReactNode;
  noParkLabel: string;
  noStageLabel: string;
  noBreedLabel: string;
  /** Gender column's empty label; it used to reuse the breed column's "No breed". */
  noSexLabel?: string;
  noShedLabel: string;
  stages: StageOption[];
  /**
   * Stage code -> the label a reader sees, built from the response's own stage facet.
   *
   * The decision of which codes read as words lives in the BACKEND
   * (counts/domain.StageDisplayLabel); this map just carries its answer, so the table, the chart
   * and the Stage filter cannot drift into three spellings of one stage. A code missing from the
   * map renders as itself.
   */
  stageLabels: ReadonlyMap<string, string>;
  breeds: InlineChoice[];
  genders: InlineChoice[];
  retagEnabled: boolean;
  retagDisabledReason: string;
}) {
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  const genderLabels = useMemo(() => new Map(genders.map((g) => [g.value, g.label] as const)), [genders]);
  const visibleKeys = useMemo(() => contract.columns.filter((column) => column.visible).map((column) => column.key), [contract]);

  function toggle(pen: CountsBreakdownPenRow) {
    const id = penRowId(pen);
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }
  const everyOpen = allOpen(pens, open);

  const penLabel = useMemo(
    () => (pen: CountsBreakdownPenRow) =>
      pen.operational_location_display ||
      operationalLocationLabel({ shedName: pen.shed_label, partitionLabel: pen.partition_label }) || // operational-location:ignore: owner=ravi issue=OL-FE-HELPER scope=shared-helper-call-not-local-sql-case expiry=2026-11-30
      noShedLabel,
    [noShedLabel],
  );

  // One composition cell, template list-cell anatomy (TR2-P1-9: no chip clouds; guard:
  // breakdown-cells-no-chips): the largest bucket as the primary line, the next ones as ONE
  // secondary caption line, largest first as the backend orders them. Past COMPOSITION_SHOWN the
  // caption ends "+N" and its title lists every bucket; every bucket is also its own row when the
  // pen opens, so nothing is unreadable.
  function composition(
    points: CountsBreakdownPoint[],
    emptyLabel: string,
    vocabulary: ReadonlyMap<string, string> = genderLabels,
  ) {
    if (points.length === 0) return <Box component="span" sx={{ color: "text.secondary" }}>{emptyLabel}</Box>;
    const text = (point: CountsBreakdownPoint) => `${pointLabel(point, emptyLabel, vocabulary)} ${point.count}`;
    if (points.length === 1) return <span>{pointLabel(points[0], emptyLabel, vocabulary)}</span>;
    const [first, ...others] = points;
    const shown = others.slice(0, COMPOSITION_SHOWN - 1);
    const rest = others.slice(COMPOSITION_SHOWN - 1);
    return (
      <Stack component="span" sx={{ minWidth: 0 }} title={points.map(text).join(" · ")}>
        <Box component="span" sx={{ typography: "body2" }}>{text(first)}</Box>
        <Box component="span" sx={{ typography: "body2", color: "text.disabled" }}>
          {shown.map(text).join(" · ")}
          {rest.length ? ` · +${rest.length}` : ""}
        </Box>
      </Stack>
    );
  }

  const columns = useMemo(
    () =>
      columnsFromContract<CountsBreakdownPenRow>(contract, {
        farm: {
          cell: (pen) => pen.park_label || noParkLabel,
          sortValue: (pen) => pen.park_label || noParkLabel,
          meta: { cellStyle: { color: "var(--palette-text-secondary)" } },
        },
        shed: {
          cell: (pen) => {
            const isOpen = open.has(penRowId(pen));
            const combos = pen.rows.length;
            return (
              // Template collapsible row (order-table-row): the expand IconButton with the rotating
              // arrow, the pen name over its combination count.
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <IconButton
                  size="small"
                  color={isOpen ? "inherit" : "default"}
                  aria-expanded={isOpen}
                  aria-controls={penDetailDomId(pen)}
                  aria-label={`${isOpen ? copy(pageContract, "action.expand.close_aria") : copy(pageContract, "action.expand.open_aria")} · ${penLabel(pen)}`}
                  onClick={(event) => {
                    // The row itself toggles too; stop here so one click is one toggle.
                    event.stopPropagation();
                    toggle(pen);
                  }}
                  sx={{ ...(isOpen ? { bgcolor: "action.hover" } : {}) }}
                >
                  <Iconify icon="eva:arrow-ios-downward-fill" sx={{ transition: "transform .15s", ...(isOpen ? {} : { transform: "rotate(-90deg)" }) }} />
                </IconButton>
                <Stack sx={{ minWidth: 0 }}>
                  <Typography variant="subtitle2" component="span" sx={{ whiteSpace: "nowrap" }}>{penLabel(pen)}</Typography>
                  <Typography variant="caption" component="span" sx={{ color: "text.secondary" }}>
                    {combos} {copy(pageContract, combos === 1 ? "detail.combinations_one" : "detail.combinations_many")}
                  </Typography>
                </Stack>
              </Box>
            );
          },
          sortValue: (pen) => penLabel(pen),
        },
        stage: {
          // ONE stage in the pen: the same inline retag editor the combination table carries,
          // which already writes the whole pen — so the pen line is where it belongs. A mixed
          // pen shows its mix; retagging one slice of it happens in the opened rows.
          cell: (pen) =>
            pen.stages.length === 1 && pen.shed_id ? (
              <span onClick={(event) => event.stopPropagation()} onDoubleClick={(event) => event.stopPropagation()}>
                <ShedTagEditor
                  pageContract={pageContract}
                  shedId={pen.shed_id}
                  partitionLabel={pen.partition_label ?? ""}
                  currentTag={pen.stages[0].key}
                  currentTagLabel={stageLabels.get(pen.stages[0].key) ?? stageLabel(pen.stages[0].label)}
                  emptyLabel={noStageLabel}
                  stages={stages}
                  enabled={retagEnabled}
                  disabledReason={retagDisabledReason}
                />
              </span>
            ) : pen.stages.length === 0 && pen.shed_id ? (
              // An EMPTY pen: no residents, so no resident stage to show. Its editor reads the
              // pen's AUTHORED tag instead -- the one newborn placement reads -- which is why an
              // empty pen is listed at all: a pen nobody can tag is a pen no kid can be born into.
              <span onClick={(event) => event.stopPropagation()} onDoubleClick={(event) => event.stopPropagation()}>
                <ShedTagEditor
                  pageContract={pageContract}
                  shedId={pen.shed_id}
                  partitionLabel={pen.partition_label ?? ""}
                  currentTag={pen.authored_stage}
                  currentTagLabel={pen.authored_stage_label || stageLabels.get(pen.authored_stage) || undefined}
                  emptyLabel={noStageLabel}
                  stages={stages}
                  enabled={retagEnabled}
                  disabledReason={retagDisabledReason}
                />
              </span>
            ) : (
              composition(pen.stages, noStageLabel, stageLabels)
            ),
          sortValue: (pen) => dominantKey(pen.stages) || pen.authored_stage || noStageLabel,
        },
        breed: {
          cell: (pen) => composition(pen.breeds, noBreedLabel),
          sortValue: (pen) => dominantKey(pen.breeds) || noBreedLabel,
        },
        gender: {
          cell: (pen) => composition(pen.sexes, noSexLabel ?? noBreedLabel),
          sortValue: (pen) => dominantKey(pen.sexes),
        },
        // Female · Male for EVERY row, in the same shape as Kids · Adults beside it.
        //
        // Read off the pen's own `sexes` composition, which the backend already sends and the
        // Gender column already renders -- so no new number is derived here and the two columns
        // cannot disagree. It is deliberately not the Gender column with counts bolted on: that
        // column shows counts only where a pen holds more than one sex, so a single-sex pen read
        // "Male" with no figure at all and a reader had to look across to Count to learn it.
        //
        // A sex the register holds as neither female nor male is carried as a third figure rather
        // than dropped, so the row's two columns still add up to its Count.
        female_male: {
          cell: (pen) => {
            const of = (key: string) =>
              pen.sexes.find((point: CountsBreakdownPoint) => point.key === key)?.count ?? 0;
            const other = pen.count - of("female") - of("male");
            return (
              <Split parts={[[of("female"), copy(pageContract, "label.female_short")], [of("male"), copy(pageContract, "label.male_short")], ...(other > 0 ? [[other, copy(pageContract, "label.sex_other_short")] as const] : [])]} />
            );
          },
          sortValue: (pen) => pen.sexes.find((point: CountsBreakdownPoint) => point.key === "female")?.count ?? 0,
        },
        kids_adults: {
          cell: (pen) => <Split parts={[[pen.kid_count, copy(pageContract, "label.kid_short")], [pen.adult_count, copy(pageContract, "label.adult_short")]]} />,
        },
        count: {
          cell: (pen) => pen.count,
          sortValue: (pen) => pen.count,
          meta: { align: "right", cellStyle: { fontWeight: 600 } },
        },
      }),
    // `open` is read inside the shed cell for the caret state, so the columns rebuild on toggle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [contract, pageContract, open, noParkLabel, noStageLabel, noBreedLabel, penLabel, stages, retagEnabled, retagDisabledReason, genderLabels, stageLabels],
  );

  return (
    <>
      {/* Table toolbar: the pen count on the left, expand/collapse as a soft kit button on the
          right — the same row anatomy as every other table card, not a lone button. */}
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, px: 3, py: 1.5 }}>
        <Typography variant="body2" sx={{ color: "text.secondary", flex: "1 1 auto" }}>{pens.length} {copy(pageContract, "table.pens.noun")}{pens.length === 1 ? "" : "s"}</Typography>
        <Button
          type="button"
          variant="soft"
          color="inherit"
          size="small"
          disabled={pens.length === 0}
          onClick={() => setOpen(everyOpen ? new Set() : new Set(pens.map(penRowId)))}
        >
          {everyOpen ? copy(pageContract, "action.collapse_all") : copy(pageContract, "action.expand_all")}
        </Button>
      </Box>
      <DataTable
        className="counts-breakdown-table counts-pens-table"
        ariaLabel={ariaLabel}
        columns={columns}
        data={pens}
        getRowId={penRowId}
        empty={empty}
        footer={footer}
        expandable={{
          isOpen: (pen) => open.has(penRowId(pen)),
          onToggle: toggle,
          // The opened rows are the pen's exact stage x breed x gender combinations, rendered on
          // the SAME column grid as the pen line so every value sits under its own heading, with
          // the same inline editors the combination table carries.
          render: (pen) =>
            pen.rows.map((row, index) => {
              const last = index === pen.rows.length - 1;
              const cells: Record<string, React.ReactNode> = {
                farm: index === 0 ? <Typography variant="caption" sx={{ color: "text.secondary", whiteSpace: "nowrap" }}>{copy(pageContract, "detail.title")}</Typography> : null,
                shed: null,
                stage: row.shed_id ? (
                  <ShedTagEditor
                    pageContract={pageContract}
                    shedId={row.shed_id}
                    partitionLabel={row.partition_label ?? ""}
                    currentTag={row.management_stage}
                    currentTagLabel={stageLabels.get(row.management_stage) ?? stageLabel(row.management_stage)}
                    emptyLabel={noStageLabel}
                    stages={stages}
                    enabled={retagEnabled}
                    disabledReason={retagDisabledReason}
                  />
                ) : (
                  (stageLabels.get(row.management_stage) ?? stageLabel(row.management_stage)) || noStageLabel
                ),
                breed: row.shed_id ? (
                  <CensusValueEditor
                    pageContract={pageContract}
                    slice={sliceOf(row)}
                    field="breed"
                    current={row.breed}
                    emptyLabel={noBreedLabel}
                    choices={breeds}
                    enabled={retagEnabled}
                    disabledReason={retagDisabledReason}
                  />
                ) : (
                  row.breed || noBreedLabel
                ),
                gender: row.shed_id ? (
                  <CensusValueEditor
                    pageContract={pageContract}
                    slice={sliceOf(row)}
                    field="sex"
                    current={row.sex}
                    emptyLabel={noBreedLabel}
                    choices={genders}
                    enabled={retagEnabled}
                    disabledReason={retagDisabledReason}
                    renderCurrent={(value) => genderLabels.get(value) ?? value}
                  />
                ) : (
                  genderLabels.get(row.sex) ?? row.sex
                ),
                // A grain row is ONE sex by construction, so its split is its own count under
                // its own sex -- filled in rather than left blank, so a reader running down the
                // column never meets a hole where a number belongs.
                female_male: <Split parts={[[row.sex === "female" ? row.count : 0, copy(pageContract, "label.female_short")], [row.sex === "male" ? row.count : 0, copy(pageContract, "label.male_short")]]} />,
                kids_adults: null,
                count: row.count,
              };
              // Template collapse content (order-table-row): the pen's combinations on the neutral
              // ground under the line, dashed dividers, the last one closing the group.
              return (
                <TableRow
                  key={`${row.management_stage}|${row.breed}|${row.sex}`}
                  id={index === 0 ? penDetailDomId(pen) : undefined}
                  aria-label={index === 0 ? `${copy(pageContract, "detail.aria")} · ${penLabel(pen)}` : undefined}
                  sx={{ bgcolor: "background.neutral", ...(last ? {} : { "& td": { borderBottomStyle: "dashed" } }) }}
                >
                  {visibleKeys.map((key) => (
                    <TableCell key={key} align={key === "count" ? "right" : undefined} sx={{ py: 1, typography: "body2" }}>
                      {cells[key] ?? null}
                    </TableCell>
                  ))}
                </TableRow>
              );
            }),
        }}
      />
    </>
  );
}
