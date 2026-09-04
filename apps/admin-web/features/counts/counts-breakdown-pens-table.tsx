"use client";

import { useMemo, useState } from "react";
import { ChevronRight } from "lucide-react";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { copy, type AdminUiPageContract, type AdminUiTableContract } from "@/lib/admin-ui-contract";
import { operationalLocationLabel } from "@/lib/operational-location";

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

// sliceOf names the row the way the correction write matches it. Every field participates in the
// predicate, so this must stay a faithful copy of the row rather than a convenient subset.
function sliceOf(row: GrainRow): CensusSlice {
  return {
    shedId: row.shed_id ?? "",
    partitionLabel: row.partition_label ?? "",
    managementStage: row.management_stage,
    breed: row.breed,
    sex: row.sex === "male" ? "male" : "female",
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
  noShedLabel,
  stages,
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
  noShedLabel: string;
  stages: StageOption[];
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

  // One composition cell. A single bucket reads as plain text (its count IS the pen's count);
  // several read as chips, each carrying its own count, largest first as the backend orders them.
  function composition(points: CountsBreakdownPoint[], emptyLabel: string, tone?: (key: string) => string) {
    if (points.length === 0) return <span className="muted">{emptyLabel}</span>;
    if (points.length === 1) return <span>{pointLabel(points[0], emptyLabel, genderLabels)}</span>;
    return (
      <span className="dimchips">
        {points.map((point) => (
          <span key={point.key || "__blank"} className={`dimchip${tone ? ` ${tone(point.key)}` : ""}`}>
            <span>{pointLabel(point, emptyLabel, genderLabels)}</span>
            <b>{point.count}</b>
          </span>
        ))}
      </span>
    );
  }

  const columns = useMemo(
    () =>
      columnsFromContract<CountsBreakdownPenRow>(contract, {
        farm: {
          cell: (pen) => pen.park_label || noParkLabel,
          sortValue: (pen) => pen.park_label || noParkLabel,
          meta: { cellClassName: "muted" },
        },
        shed: {
          cell: (pen) => {
            const isOpen = open.has(penRowId(pen));
            const combos = pen.rows.length;
            return (
              <span className="xcell">
                <button
                  type="button"
                  className="xtoggle"
                  aria-expanded={isOpen}
                  aria-controls={penDetailDomId(pen)}
                  aria-label={`${isOpen ? copy(pageContract, "action.expand.close_aria") : copy(pageContract, "action.expand.open_aria")} · ${penLabel(pen)}`}
                  onClick={(event) => {
                    // The row itself toggles too; stop here so one click is one toggle.
                    event.stopPropagation();
                    toggle(pen);
                  }}
                >
                  <span className="caret" aria-hidden="true">
                    <ChevronRight size={12} strokeWidth={3} />
                  </span>
                  <span className="xname">{penLabel(pen)}</span>
                </button>
                <span className="small muted xsub">
                  {combos} {copy(pageContract, combos === 1 ? "detail.combinations_one" : "detail.combinations_many")}
                </span>
              </span>
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
                  emptyLabel={noStageLabel}
                  stages={stages}
                  enabled={retagEnabled}
                  disabledReason={retagDisabledReason}
                />
              </span>
            ) : (
              composition(pen.stages, noStageLabel)
            ),
          sortValue: (pen) => dominantKey(pen.stages) || noStageLabel,
        },
        breed: {
          cell: (pen) => composition(pen.breeds, noBreedLabel),
          sortValue: (pen) => dominantKey(pen.breeds) || noBreedLabel,
        },
        gender: {
          cell: (pen) => composition(pen.sexes, noBreedLabel, (key) => (key === "female" ? "f" : key === "male" ? "m" : "")),
          sortValue: (pen) => dominantKey(pen.sexes),
        },
        kids_adults: {
          cell: (pen) => (
            <span className="agesplit">
              <b>{pen.kid_count}</b> <small>{copy(pageContract, "label.kid_short")}</small>
              <span className="muted"> · </span>
              <b>{pen.adult_count}</b> <small>{copy(pageContract, "label.adult_short")}</small>
            </span>
          ),
        },
        count: {
          cell: (pen) => pen.count,
          sortValue: (pen) => pen.count,
          meta: { align: "right", cellStyle: { fontWeight: 700, color: "var(--brand-d)", fontSize: 15 } },
        },
      }),
    // `open` is read inside the shed cell for the caret state, so the columns rebuild on toggle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [contract, pageContract, open, noParkLabel, noStageLabel, noBreedLabel, penLabel, stages, retagEnabled, retagDisabledReason, genderLabels],
  );

  return (
    <>
      <div className="xbar">
        <span className="small muted">{copy(pageContract, "action.expand.hint")}</span>
        <span className="sp" />
        <button
          type="button"
          className="btn sm"
          disabled={pens.length === 0}
          onClick={() => setOpen(everyOpen ? new Set() : new Set(pens.map(penRowId)))}
        >
          {everyOpen ? copy(pageContract, "action.collapse_all") : copy(pageContract, "action.expand_all")}
        </button>
      </div>
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
                farm: index === 0 ? <span className="xsplit">{copy(pageContract, "detail.title")}</span> : null,
                shed: null,
                stage: row.shed_id ? (
                  <ShedTagEditor
                    pageContract={pageContract}
                    shedId={row.shed_id}
                    partitionLabel={row.partition_label ?? ""}
                    currentTag={row.management_stage}
                    emptyLabel={noStageLabel}
                    stages={stages}
                    enabled={retagEnabled}
                    disabledReason={retagDisabledReason}
                  />
                ) : (
                  row.management_stage || noStageLabel
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
                kids_adults: null,
                count: <b className="xcount">{row.count}</b>,
              };
              return (
                <tr
                  key={`${row.management_stage}|${row.breed}|${row.sex}`}
                  className={`xdetail${index === 0 ? " first" : ""}${last ? " last" : ""}`}
                  id={index === 0 ? penDetailDomId(pen) : undefined}
                  aria-label={index === 0 ? `${copy(pageContract, "detail.aria")} · ${penLabel(pen)}` : undefined}
                >
                  {visibleKeys.map((key) => (
                    <td key={key} style={key === "count" ? { textAlign: "right" } : undefined}>
                      {cells[key] ?? null}
                    </td>
                  ))}
                </tr>
              );
            }),
        }}
      />
    </>
  );
}
