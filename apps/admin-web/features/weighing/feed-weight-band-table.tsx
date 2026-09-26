"use client";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { stageLabel } from "@/lib/stage-labels";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";

const BAND_STEPS = ["under_15", "15_20", "20_25", "25_30", "30_35", "35_plus"] as const;

/**
 * A bracket as label plus the artifact's six-step bar: one step per 5 kg bracket, filled up to
 * this one, so a reader can place a row on the scale without reading the number. Colours are the
 * theme's line and brand tokens; nothing new.
 */
export function BandCell({ band, label }: { band: string; label: string }) {
  const filled = Math.max(0, BAND_STEPS.indexOf(band as (typeof BAND_STEPS)[number]) + 1);
  return (
    <span className="wt-bandc">
      <span className="wt-bandbar" aria-hidden>
        {BAND_STEPS.map((step, index) => (
          <i key={step} className={index < filled ? "on" : undefined} />
        ))}
      </span>
      <b>{label}</b>
    </span>
  );
}

/**
 * One row of the Weight-wise tab's feed table (the Matched view), already RESOLVED by the server
 * component: the band, source and feed type are the backend's stable keys with their farm words
 * handed down beside them, and every figure is a number so the cell can round it. This file names
 * no label of its own.
 */
export type FeedWeightBandTableRow = {
  key: string;
  park: string;
  weightSource: string;
  weightSourceLabel: string;
  /** The desktop table's compact pill ("Lump" / "Animal"); the full label is its title. */
  weightSourceShort: string;
  band: string;
  bandLabel: string;
  pen: string;
  group: string;
  gender: string;
  breed: string;
  feedType: string;
  feedTypeLabel: string;
  /** The desktop table's compact pill ("Exp." / "Normal"); the full label is its title. */
  feedTypeShort: string;
  feedGiven: string;
  penKgPerDay: number;
  weightAnimals: number;
  averageKg: number;
  /** Weighed animals of this band that have since exited the register, by bucket; shown as a note under Wt n. */
  exitedSold: number;
  exitedDied: number;
  exitedOther: number;
  /** Whether the read counted those animals in Wt n ("incl.") or left them out ("+"). */
  includeExited: boolean;
  /** Opens the exited panel scoped to this pen × bracket; absent when nothing left. */
  exitHref?: string;
};

export type FeedWeightBandTableLabels = {
  ariaLabel: string;
  /** A row whose animals do not resolve in the register reads as absence, never as a blank cell. */
  noGender: string;
  sold: string;
  died: string;
  other: string;
  /** The prefix when the exited animals are counted in: "incl. 3 sold". */
  incl: string;
  empty: React.ReactNode;
};

const kg = (value: number, digits = 1) =>
  value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });

function FeedTypeTag({ feedType, label, title }: { feedType: string; label: string; title?: string }) {
  return (
    <Tag tone={feedType === "experiment" ? "pur" : "mut"} title={title}>
      {label}
    </Tag>
  );
}

/** "Bhusa 250 g/head + Kids Concentrate 1450 g/head" as one line per item, never truncated. */
function FeedGivenLines({ value }: { value: string }) {
  const parts = value.split(" + ").filter(Boolean);
  return (
    <span className="wt-feedband-feed">
      {parts.map((part, index) => (
        <span key={part + index} className="wt-feedband-feedline">
          {index > 0 ? "+ " : ""}
          {part}
        </span>
      ))}
    </span>
  );
}

function ExitNotes({ row, labels }: { row: FeedWeightBandTableRow; labels: FeedWeightBandTableLabels }) {
  const notes: string[] = [];
  const prefix = row.includeExited ? `${labels.incl} ` : "+";
  if (row.exitedSold > 0) notes.push(`${prefix}${row.exitedSold.toLocaleString("en-IN")} ${labels.sold}`);
  if (row.exitedDied > 0) notes.push(`${prefix}${row.exitedDied.toLocaleString("en-IN")} ${labels.died}`);
  if (row.exitedOther > 0) notes.push(`${prefix}${row.exitedOther.toLocaleString("en-IN")} ${labels.other}`);
  return (
    <>
      {notes.map((note) =>
        row.exitHref ? (
          // A click opens the exited panel for THIS pen and bracket, the app's local drawer: no
          // route re-run, filters untouched.
          <LocalOverlayLink key={note} href={row.exitHref} className="wt-feedband-gone" scroll={false}>
            {note}
          </LocalOverlayLink>
        ) : (
          <span key={note} className="wt-feedband-gone">
            {note}
          </span>
        ),
      )}
    </>
  );
}

/**
 * The feed-by-weight-band table on TanStack, the pens-table shape. Columns come from the page's
 * `feed-weight-band` contract, so a column the backend drops never renders. Rows render in the
 * order served: park, lump-sum rows before per-animal rows, then pen, band, shed tag; the Lump sum
 * / Per animal pills are the Pens table's own, so the two ways of weighing read apart at a glance.
 */
export function FeedWeightBandTable({
  contract,
  rows,
  labels,
}: {
  contract: AdminUiTableContract;
  rows: FeedWeightBandTableRow[];
  labels: FeedWeightBandTableLabels;
}) {
  const columns = columnsFromContract<FeedWeightBandTableRow>(contract, {
    park: { cell: (row) => <Tag tone="mut">{row.park}</Tag> },
    weight_source: {
      cell: (row) => (
        <Tag tone={row.weightSource === "per_animal" ? "info" : "mut"} title={row.weightSourceLabel}>
          {row.weightSourceShort}
        </Tag>
      ),
    },
    // Label only on the desktop table; the six-step bar rides under the label on the phone cards.
    band: { cell: (row) => <b className="wt-feedband-bandlabel">{row.bandLabel}</b> },
    pen: { cell: (row) => <b>{row.pen}</b>, meta: { cellClassName: "wt-feedband-pen" } },
    group: { cell: (row) => stageLabel(row.group), meta: { cellClassName: "wt-feedband-narrow" } },
    gender: { cell: (row) => (row.gender ? row.gender : <span className="muted">{labels.noGender}</span>), meta: { cellClassName: "wt-feedband-narrow" } },
    breed: { cell: (row) => row.breed, meta: { cellClassName: "wt-feedband-breed" } },
    feed_type: { cell: (row) => <FeedTypeTag feedType={row.feedType} label={row.feedTypeShort} title={row.feedTypeLabel} /> },
    feed_given: { cell: (row) => <FeedGivenLines value={row.feedGiven} />, meta: { cellClassName: "wt-feedband-feedcell" } },
    pen_kg_per_day: {
      cell: (row) => kg(row.penKgPerDay),
      meta: { cellClassName: "num wt-feedband-num" },
    },
    weight_animals: {
      cell: (row) => (
        <>
          {row.weightAnimals.toLocaleString("en-IN")}
          <ExitNotes row={row} labels={labels} />
        </>
      ),
      meta: { cellClassName: "num wt-feedband-num" },
    },
    average_weight: {
      cell: (row) => kg(row.averageKg),
      meta: { cellClassName: "num wt-feedband-num" },
    },
  });
  const visible = new Set(contract.columns.filter((column) => column.visible).map((column) => column.key));

  return (
    <>
      <div className="wt-feedband-tablehost">
        <DataTable<FeedWeightBandTableRow>
          className="tbl wt-feedband"
          columns={columns}
          data={rows}
          getRowId={(row) => row.key}
          ariaLabel={labels.ariaLabel}
          empty={labels.empty}
        />
      </div>
      {/* Phone (< 768px): one stacked card per row instead of the twelve-column table, so Feed
          given and Breed read in full and nothing scrolls sideways. Same rows, same order. */}
      <ul className="wt-feedband-cards" aria-label={labels.ariaLabel}>
        {rows.length === 0 ? <li className="wt-feedband-cardempty muted small">{labels.empty}</li> : null}
        {rows.map((row) => (
          <li key={row.key} className="wt-feedband-cardrow">
            <div className="wt-feedband-cardline">
              <b className="wt-feedband-cardpen">{row.pen}</b>
              {visible.has("park") ? <Tag tone="mut">{row.park}</Tag> : null}
              <Tag tone={row.weightSource === "per_animal" ? "info" : "mut"}>{row.weightSourceLabel}</Tag>
            </div>
            <div className="wt-feedband-cardline">
              <BandCell band={row.band} label={row.bandLabel} />
            </div>
            <div className="wt-feedband-cardline wt-feedband-cardfeed">
              <FeedTypeTag feedType={row.feedType} label={row.feedTypeLabel} />
              <FeedGivenLines value={row.feedGiven} />
            </div>
            <div className="wt-feedband-cardline muted small">
              {row.breed} · {row.group} · {row.gender || labels.noGender}
            </div>
            <div className="wt-feedband-cardline wt-feedband-cardnums">
              <span>
                <span className="muted small">{contract.columns.find((column) => column.key === "pen_kg_per_day")?.label} </span>
                <b>{kg(row.penKgPerDay)}</b>
              </span>
              <span>
                <span className="muted small">{contract.columns.find((column) => column.key === "weight_animals")?.label} </span>
                <b>{row.weightAnimals.toLocaleString("en-IN")}</b> <ExitNotes row={row} labels={labels} />
              </span>
              <span>
                <span className="muted small">{contract.columns.find((column) => column.key === "average_weight")?.label} </span>
                <b>{kg(row.averageKg)}</b>
              </span>
            </div>
          </li>
        ))}
      </ul>
    </>
  );
}

/** One feed rollup with no qualifying weighing in the period (the Not shown view). */
export type FeedWeightBandUnmatchedRow = {
  key: string;
  park: string;
  pen: string;
  shedTag: string;
  ration: string;
  breed: string;
  feedType: string;
  feedTypeLabel: string;
  feedGiven: string;
  penKgPerDay: number;
};

export function FeedWeightBandUnmatchedTable({
  contract,
  rows,
  labels,
}: {
  contract: AdminUiTableContract;
  rows: FeedWeightBandUnmatchedRow[];
  labels: { ariaLabel: string; empty: React.ReactNode };
}) {
  const columns = columnsFromContract<FeedWeightBandUnmatchedRow>(contract, {
    park: { cell: (row) => row.park },
    pen: { cell: (row) => <b>{row.pen}</b> },
    shed_tag: { cell: (row) => stageLabel(row.shedTag) },
    ration: { cell: (row) => stageLabel(row.ration) },
    breed: { cell: (row) => <span title={row.breed}>{row.breed}</span>, meta: { cellClassName: "wt-feedband-wrap" } },
    feed_type: { cell: (row) => <FeedTypeTag feedType={row.feedType} label={row.feedTypeLabel} /> },
    feed_given: { cell: (row) => <span className="wt-feedband-feed" title={row.feedGiven}>{row.feedGiven}</span> },
    pen_kg_per_day: { cell: (row) => kg(row.penKgPerDay), meta: { cellClassName: "num" } },
  });
  return (
    <DataTable<FeedWeightBandUnmatchedRow>
      className="tbl wt-feedband"
      columns={columns}
      data={rows}
      getRowId={(row) => row.key}
      ariaLabel={labels.ariaLabel}
      empty={labels.empty}
    />
  );
}
