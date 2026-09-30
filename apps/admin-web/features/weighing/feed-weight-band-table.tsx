"use client";

import Box from "@mui/material/Box";
import Link from "@mui/material/Link";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import type { Theme } from "@mui/material/styles";
import type { SystemStyleObject } from "@mui/system";
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
export function BandCell({ band, label, wide = false }: { band: string; label: string; wide?: boolean }) {
  const barWidth = wide ? 60 : 26;
  const filled = Math.max(0, BAND_STEPS.indexOf(band as (typeof BAND_STEPS)[number]) + 1);
  return (
    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.625, whiteSpace: "nowrap" }}>
      <Box
        component="span"
        aria-hidden
        data-band-bar=""
        sx={{ display: "inline-flex", gap: 0.25, width: barWidth, flexShrink: 0, flexGrow: 0 }}
      >
        {BAND_STEPS.map((step, index) => (
          <Box
            key={step}
            component="span"
            data-on={index < filled ? "true" : undefined}
            sx={{ display: "block", flex: 1, height: "calc(var(--sp-1h) / 2)", borderRadius: "calc(var(--r-sm) / 3)", bgcolor: index < filled ? "primary.dark" : "divider" }}
          />
        ))}
      </Box>
      <b>{label}</b>
    </Box>
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

/** Compact type for the twelve-column table, on the type-scale tokens. */
const FS = {
  xxsh: "calc(var(--fs-caption) * 0.875)", // 10.5
  xs: "calc(var(--fs-caption) * 11 / 12)", // 11
  sm: "var(--fs-caption)", // 12
  smh: "calc(var(--fs-caption) * 25 / 24)", // 12.5
  md: "var(--fs-button-sm)", // 13
  body: "var(--fs-body2)", // 14
} as const;

/**
 * Per-column widths as cell sx (DataTable meta.cellStyle). The limits are CSS variables the table
 * wrapper sets per breakpoint, so twelve columns fit 1440 and 1280 with no scroll: feed and breed
 * never truncate on a laptop, they wrap a line more.
 */
const COL = {
  pen: { whiteSpace: "normal", maxWidth: "var(--fb-pen-max)", lineHeight: 1.25 },
  narrow: { whiteSpace: "normal", maxWidth: "var(--fb-narrow-max)", lineHeight: 1.25 },
  breed: { whiteSpace: "normal", minWidth: "var(--fb-breed-min)", maxWidth: "var(--fb-breed-max)", lineHeight: 1.3 },
  num: { fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap" },
} as const satisfies Record<string, React.CSSProperties>;

const feedBandTableSx = (theme: Theme): SystemStyleObject<Theme> => ({
  "--fb-pen-max": theme.spacing(16.5),
  "--fb-narrow-max": theme.spacing(10.5),
  "--fb-breed-min": theme.spacing(21.25),
  "--fb-breed-max": theme.spacing(26.25),
  // :where keeps these at the wrapper's own weight, so a column's cell sx (COL) overrides them.
  "& :where(td.MuiTableCell-root)": { whiteSpace: "nowrap", verticalAlign: "middle", fontSize: FS.smh, py: 1, px: 0.5 },
  "& :where(th.MuiTableCell-root)": { fontSize: FS.xxsh, whiteSpace: "normal", lineHeight: 1.2, px: 0.5 },
  "& [data-feed-given]": { minWidth: 230 },
  // A 1280 laptop: the same twelve columns, still no scroll and no truncation (a line more of wrap).
  [theme.breakpoints.down(1366)]: {
    "--fb-pen-max": theme.spacing(12),
    "--fb-narrow-max": theme.spacing(9),
    "--fb-breed-min": theme.spacing(17.5),
    "--fb-breed-max": theme.spacing(20),
    "& [data-feed-given]": { minWidth: 188 },
    "& :where(td.MuiTableCell-root, th.MuiTableCell-root)": { px: 0.375 },
  },
});

/** "Bhusa 250 g/head + Kids Concentrate 1450 g/head" as one line per item, never truncated. */
function FeedGivenLines({ value, card = false }: { value: string; card?: boolean }) {
  const parts = value.split(" + ").filter(Boolean);
  return (
    <Box
      component="span"
      data-feed-given=""
      sx={
        card
          ? { display: "block", whiteSpace: "normal", minWidth: 0, color: "text.primary", fontSize: FS.md, lineHeight: 1.35, flex: "1 1 100%" }
          : { display: "block", whiteSpace: "normal", color: "text.secondary", fontSize: FS.sm, lineHeight: 1.3 }
      }
    >
      {parts.map((part, index) => (
        <Box component="span" key={part + index} sx={{ display: "block" }}>
          {index > 0 ? "+ " : ""}
          {part}
        </Box>
      ))}
    </Box>
  );
}

/** "+N sold" under the head count: a small note in the theme's warning hue; a link when it opens the panel. */
const exitNoteSx = (card: boolean) =>
  card
    ? // Phone card: a 44px tap target in the row of figures.
      { display: "inline-flex", alignItems: "center", minHeight: "var(--tap-min)", px: 0.75, fontSize: FS.sm, color: "warning.main" }
    : { display: "block", whiteSpace: "nowrap", fontSize: FS.xs, lineHeight: 1.3, color: "warning.main" };

function ExitNotes({ row, labels, card = false }: { row: FeedWeightBandTableRow; labels: FeedWeightBandTableLabels; card?: boolean }) {
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
          <Link
            key={note}
            component={LocalOverlayLink}
            href={row.exitHref}
            scroll={false}
            data-feedband-exit=""
            underline="hover"
            sx={exitNoteSx(card)}
          >
            {note}
          </Link>
        ) : (
          <Box component="span" key={note} sx={exitNoteSx(card)}>
            {note}
          </Box>
        ),
      )}
    </>
  );
}

const cardCaption = (text: React.ReactNode) => (
  <Typography component="span" variant="caption" sx={{ color: "text.secondary" }}>
    {text}{" "}
  </Typography>
);
const cardLineSx = { display: "flex", flexWrap: "wrap", alignItems: "center", columnGap: 1, rowGap: 0.75, minWidth: 0, overflowWrap: "anywhere" } as const;

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
    band: { cell: (row) => <Box component="b" sx={{ whiteSpace: "nowrap" }}>{row.bandLabel}</Box> },
    pen: { cell: (row) => <b>{row.pen}</b>, meta: { cellStyle: COL.pen } },
    group: { cell: (row) => stageLabel(row.group), meta: { cellStyle: COL.narrow } },
    gender: { cell: (row) => (row.gender ? row.gender : <Box component="span" sx={{ color: "text.secondary" }}>{labels.noGender}</Box>), meta: { cellStyle: COL.narrow } },
    breed: { cell: (row) => row.breed, meta: { cellStyle: COL.breed } },
    feed_type: { cell: (row) => <FeedTypeTag feedType={row.feedType} label={row.feedTypeShort} title={row.feedTypeLabel} /> },
    feed_given: { cell: (row) => <FeedGivenLines value={row.feedGiven} /> },
    pen_kg_per_day: {
      cell: (row) => kg(row.penKgPerDay),
      meta: { cellStyle: COL.num, align: "right" },
    },
    weight_animals: {
      cell: (row) => (
        <>
          {row.weightAnimals.toLocaleString("en-IN")}
          <ExitNotes row={row} labels={labels} />
        </>
      ),
      meta: { cellStyle: COL.num, align: "right" },
    },
    average_weight: {
      cell: (row) => kg(row.averageKg),
      meta: { cellStyle: COL.num, align: "right" },
    },
  });
  const visible = new Set(contract.columns.filter((column) => column.visible).map((column) => column.key));

  return (
    <>
      <Box
        sx={(theme): SystemStyleObject<Theme> => ({
          ...feedBandTableSx(theme),
          [theme.breakpoints.down(768)]: { display: "none" },
        })}
      >
        <DataTable<FeedWeightBandTableRow>
          columns={columns}
          data={rows}
          getRowId={(row) => row.key}
          ariaLabel={labels.ariaLabel}
          empty={labels.empty}
        />
      </Box>
      {/* Phone (< 768px): one stacked card per row instead of the twelve-column table, so Feed
          given and Breed read in full and nothing scrolls sideways. Same rows, same order. */}
      <Stack
        component="ul"
        aria-label={labels.ariaLabel}
        spacing={1}
        sx={(theme) => ({ display: "none", listStyle: "none", m: 0, px: 1.5, pb: 1, [theme.breakpoints.down(768)]: { display: "flex" } })}
      >
        {rows.length === 0 ? (
          <Typography component="li" variant="caption" sx={{ color: "text.secondary", px: 0.5, py: 1.75 }}>
            {labels.empty}
          </Typography>
        ) : null}
        {rows.map((row) => (
          <Box
            component="li"
            key={row.key}
            sx={{ border: 1, borderColor: "divider", borderRadius: "var(--r2)", bgcolor: "background.paper", px: 1.5, py: 1.25, display: "grid", gap: 0.75, minWidth: 0 }}
          >
            <Box sx={cardLineSx}>
              <Box component="b" sx={{ fontSize: FS.body, mr: "auto" }}>{row.pen}</Box>
              {visible.has("park") ? <Tag tone="mut">{row.park}</Tag> : null}
              <Tag tone={row.weightSource === "per_animal" ? "info" : "mut"}>{row.weightSourceLabel}</Tag>
            </Box>
            <Box sx={cardLineSx}>
              <BandCell band={row.band} label={row.bandLabel} wide />
            </Box>
            <Box sx={cardLineSx}>
              <FeedTypeTag feedType={row.feedType} label={row.feedTypeLabel} />
              <FeedGivenLines value={row.feedGiven} card />
            </Box>
            <Typography component="div" variant="caption" sx={{ ...cardLineSx, color: "text.secondary" }}>
              {row.breed} · {row.group} · {row.gender || labels.noGender}
            </Typography>
            <Box sx={{ ...cardLineSx, justifyContent: "space-between", columnGap: 1.5 }}>
              <Box component="span" sx={{ display: "inline-flex", flexWrap: "wrap", alignItems: "baseline", gap: 0.5 }}>
                {cardCaption(contract.columns.find((column) => column.key === "pen_kg_per_day")?.label)}
                <b>{kg(row.penKgPerDay)}</b>
              </Box>
              <Box component="span" sx={{ display: "inline-flex", flexWrap: "wrap", alignItems: "baseline", gap: 0.5 }}>
                {cardCaption(contract.columns.find((column) => column.key === "weight_animals")?.label)}
                <b>{row.weightAnimals.toLocaleString("en-IN")}</b> <ExitNotes row={row} labels={labels} card />
              </Box>
              <Box component="span" sx={{ display: "inline-flex", flexWrap: "wrap", alignItems: "baseline", gap: 0.5 }}>
                {cardCaption(contract.columns.find((column) => column.key === "average_weight")?.label)}
                <b>{kg(row.averageKg)}</b>
              </Box>
            </Box>
          </Box>
        ))}
      </Stack>
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
    breed: { cell: (row) => <span title={row.breed}>{row.breed}</span> },
    feed_type: { cell: (row) => <FeedTypeTag feedType={row.feedType} label={row.feedTypeLabel} /> },
    feed_given: {
      cell: (row) => (
        <Box component="span" data-feed-given="" title={row.feedGiven} sx={{ display: "block", whiteSpace: "normal", color: "text.secondary", fontSize: FS.sm, lineHeight: 1.3 }}>
          {row.feedGiven}
        </Box>
      ),
    },
    pen_kg_per_day: { cell: (row) => kg(row.penKgPerDay), meta: { cellStyle: COL.num, align: "right" } },
  });
  return (
    <Box sx={feedBandTableSx}>
      <DataTable<FeedWeightBandUnmatchedRow>
        columns={columns}
        data={rows}
        getRowId={(row) => row.key}
        ariaLabel={labels.ariaLabel}
        empty={labels.empty}
      />
    </Box>
  );
}
