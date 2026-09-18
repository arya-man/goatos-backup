"use client";

import { DataTable, columnsFromContract } from "@/components/data-table";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
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
  band: string;
  bandLabel: string;
  pen: string;
  group: string;
  gender: string;
  breed: string;
  feedType: string;
  feedTypeLabel: string;
  feedGiven: string;
  penKgPerDay: number;
  weightAnimals: number;
  averageKg: number;
  /** Weighed animals of this band that have since been sold or died; shown as a note under Wt n. */
  exitedSold: number;
  exitedDied: number;
  /** Whether the read counted those animals in Wt n ("incl.") or left them out ("+"). */
  includeExited: boolean;
  /** Opens the sold / dead panel scoped to this pen × bracket; absent when nothing left. */
  exitHref?: string;
};

export type FeedWeightBandTableLabels = {
  ariaLabel: string;
  /** A row whose animals do not resolve in the register reads as absence, never as a blank cell. */
  noGender: string;
  sold: string;
  died: string;
  /** The prefix when the exited animals are counted in: "incl. 3 sold". */
  incl: string;
  empty: React.ReactNode;
};

const kg = (value: number, digits = 1) =>
  value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });

function FeedTypeTag({ feedType, label }: { feedType: string; label: string }) {
  return <Tag tone={feedType === "experiment" ? "pur" : "mut"}>{label}</Tag>;
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
    park: { cell: (row) => row.park },
    weight_source: {
      cell: (row) => (
        <Tag tone={row.weightSource === "per_animal" ? "info" : "mut"}>{row.weightSourceLabel}</Tag>
      ),
    },
    band: { cell: (row) => <BandCell band={row.band} label={row.bandLabel} /> },
    pen: { cell: (row) => <b>{row.pen}</b> },
    group: { cell: (row) => row.group },
    gender: { cell: (row) => (row.gender ? row.gender : <span className="muted">{labels.noGender}</span>) },
    breed: { cell: (row) => row.breed, meta: { cellClassName: "wt-feedband-wrap" } },
    feed_type: { cell: (row) => <FeedTypeTag feedType={row.feedType} label={row.feedTypeLabel} /> },
    feed_given: { cell: (row) => <span className="wt-feedband-feed">{row.feedGiven}</span> },
    pen_kg_per_day: {
      cell: (row) => kg(row.penKgPerDay),
      meta: { cellClassName: "num" },
    },
    weight_animals: {
      cell: (row) => {
        const notes: string[] = [];
        const prefix = row.includeExited ? `${labels.incl} ` : "+";
        if (row.exitedSold > 0) notes.push(`${prefix}${row.exitedSold.toLocaleString("en-IN")} ${labels.sold}`);
        if (row.exitedDied > 0) notes.push(`${prefix}${row.exitedDied.toLocaleString("en-IN")} ${labels.died}`);
        return (
          <>
            {row.weightAnimals.toLocaleString("en-IN")}
            {notes.map((note) =>
              row.exitHref ? (
                // A click opens the sold / dead panel for THIS pen and bracket, the app's local
                // drawer: no route re-run, filters untouched.
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
      },
      meta: { cellClassName: "num" },
    },
    average_weight: {
      cell: (row) => kg(row.averageKg),
      meta: { cellClassName: "num" },
    },
  });

  return (
    <DataTable<FeedWeightBandTableRow>
      className="tbl wt-feedband"
      columns={columns}
      data={rows}
      getRowId={(row) => row.key}
      ariaLabel={labels.ariaLabel}
      empty={labels.empty}
    />
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
    shed_tag: { cell: (row) => row.shedTag },
    ration: { cell: (row) => row.ration },
    breed: { cell: (row) => row.breed, meta: { cellClassName: "wt-feedband-wrap" } },
    feed_type: { cell: (row) => <FeedTypeTag feedType={row.feedType} label={row.feedTypeLabel} /> },
    feed_given: { cell: (row) => <span className="wt-feedband-feed">{row.feedGiven}</span> },
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
