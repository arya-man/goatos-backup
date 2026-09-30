"use client";

// Route + panel loading placeholders, built from the SAME layout parts the pages render (template
// CustomBreadcrumbs anatomy, MUI Tabs, KpiGrid, Card + CardHeader, Table + TableHead, the template
// TablePaginationCustom toolbar height), with MUI Skeleton inside them — the template's
// table-skeleton / product-item-skeleton usage. A line of text is a `variant="text"` Skeleton inside a
// Box carrying the real typography, so it takes the exact line height of the text it stands in for.
// A route's loading.tsx composes these to match its page; nothing here draws a page-specific shape.

import type { ReactNode } from "react";
import Box from "@mui/material/Box";
import { PAGE_ROOT_SX } from "@/components/app/page-root";
import Card from "@mui/material/Card";
import Divider from "@mui/material/Divider";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Paper from "@mui/material/Paper";
import Skeleton from "@mui/material/Skeleton";
import Stack from "@mui/material/Stack";
import Tab from "@mui/material/Tab";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Tabs from "@mui/material/Tabs";
import type { SxProps, Theme } from "@mui/material/styles";

import Breadcrumbs from "@mui/material/Breadcrumbs";
import { CustomBreadcrumbs } from "@/components/minimal/custom-breadcrumbs";
import { BreadcrumbsSeparator } from "@/components/minimal/custom-breadcrumbs/styles";
import { KpiGrid } from "@/components/app/kpi-grid";
import { pageHeaderActionsSx, pageHeaderLayoutSx, type PageHeaderLayout } from "@/components/app/page-header";
import { orderToolbarFilterSx, orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { FILTER_SEARCH_BASIS } from "@/components/app/filter-field-widths";
import { TAP_MIN } from "@/theme/tap-target";

type Typo = "h3" | "h4" | "h5" | "h6" | "subtitle1" | "subtitle2" | "body1" | "body2" | "caption";

/** One line of text: a text Skeleton at the real typography's line height. */
export function SkeletonLine({ variant = "body2", width = "60%", sx }: { variant?: Typo; width?: number | string; sx?: SxProps<Theme> }) {
  return (
    // A paragraph box (not a div): it stands for a line of Typography, and takes that line's box, not
    // the 60%-scaled text Skeleton inside it, when the layout walk measures a free-standing line.
    <Box component="p" sx={[{ typography: variant, width, maxWidth: 1, m: 0 }, ...(Array.isArray(sx) ? sx : [sx])]}>
      <Skeleton variant="text" animation="wave" component="span" sx={{ display: "block" }} />
    </Box>
  );
}

/**
 * A control's height, as PhoneTapStyles makes it: every Button / IconButton / input is >= 44px below
 * `md`, so a button placeholder is 44 there and its desktop height from md up.
 */
const tapHeight = (desktop: number) => ({ xs: Math.max(44, desktop), md: desktop });

/** The template outlined TextField height (medium). */
const FIELD_H = 54;

const wobble = (i: number, base = 44, span = 36) => `${base + ((i * 37) % span)}%`;

/**
 * The page root while loading: the SAME root the page renders, busy for assistive tech. The default
 * `root="page-root"` twins the template `PageRoot` (components/app/page-root.tsx: sx grid, 24px block
 * rhythm, `data-page-root`, no legacy class; guard page-root-sx). `root=""` is a plain block for a
 * page whose root is a bare Box; `gap` mirrors a root that sets its own gap.
 */
export function PageSkeleton({ children, root = "page-root", gap }: { children: ReactNode; root?: "page-root" | ""; gap?: number }) {
  if (root === "page-root") {
    return (
      <Box data-page-root="" aria-busy="true" data-skel-root="" sx={gap != null ? { ...PAGE_ROOT_SX, gap } : PAGE_ROOT_SX}>
        {children}
      </Box>
    );
  }
  return (
    <Box aria-busy="true" data-skel-root="" sx={gap != null ? { display: "grid", gridTemplateColumns: "minmax(0, 1fr)", gap } : undefined}>
      {children}
    </Box>
  );
}

/**
 * Loading twin of `PageHeader`: template CustomBreadcrumbs (h4 heading, body2 crumb row, actions on
 * the right), then the header's own tab strip / toolbar slots.
 */
export function PageHeaderSkeleton({
  crumbs = true,
  crumbLink = true,
  crumbWidths = [52, 110],
  titleWidth = 260,
  actions = 0,
  actionWidths,
  actionHeights,
  mb,
  tabs,
  toolbar,
  back = false,
  layout,
}: {
  /** The SAME PageHeaderLayout constant the page passes to PageHeader (guard: page-header-layout-twin). */
  layout?: PageHeaderLayout;
  /**
   * The heading is a back link (PageHeader `backHref`: template BackLink, arrow + title). It is an
   * `a.minimal__breadcrumbs__back`, so PhoneTapStyles gives it the same 44px box below md as the page's.
   */
  back?: boolean;
  /** The page shows a crumb trail (PageHeader hides one that only repeats the title). */
  crumbs?: boolean;
  /** The parent crumb is a link (it has an href; a 44px tap target below md). */
  crumbLink?: boolean;
  /** The crumbs' rendered widths (parent, current): long crumbs wrap to a second line on a phone as the page's do. */
  crumbWidths?: [number, number];
  /**
   * The title's rendered width. On a phone the header's actions sit beside the title block when both
   * fit (a short title) and wrap under it otherwise; the placeholder must wrap the same way.
   */
  titleWidth?: number;
  /** Number of header action buttons (right-aligned, 36px template Button). */
  actions?: number;
  /** Per-action width; a string (e.g. a calc) for an action that takes its own row below md. Capped at the row. */
  actionWidths?: (number | string | Record<string, number | string>)[];
  /** Per-action height when it is not a 36px Button (a view ToggleButtonGroup: 40, 54 on a phone). */
  actionHeights?: (number | Record<string, number> | undefined)[];
  /** The page's own margin under its header when it sets one (template CustomBreadcrumbs mb { xs 3, md 5 }, minus the stack gap). */
  mb?: number | Record<string, number>;
  tabs?: ReactNode;
  toolbar?: ReactNode;
}) {
  const widths: (number | string | Record<string, number | string>)[] = actionWidths ?? Array.from({ length: actions }, (_, i) => (i === actions - 1 ? 128 : 104));
  // The verbatim template CustomBreadcrumbs, as PageHeader renders it: the heading slot (h4
  // typography, full row width) holds a text Skeleton, the crumb slot is the same MUI Breadcrumbs
  // (dot separators, body2 line) with Skeleton crumbs, the actions sit in the same right-hand Box.
  // The heading keeps the styled slot's own element (not PageHeader's h1: one h1 per document).
  return (
    <Box component="header" aria-hidden="true" data-skel="header" sx={{ display: "flex", flexDirection: "column", gap: 3, ...(mb != null ? { mb } : {}) }}>
      <CustomBreadcrumbs
        heading={
          (back ? (
            <Box component="a" aria-hidden="true" tabIndex={-1} className="minimal__breadcrumbs__back" data-skel="back-title" sx={{ display: "inline-flex", alignItems: "center", verticalAlign: "middle", maxWidth: 1 }}>
              <Skeleton variant="circular" width={18} height={18} sx={{ flexShrink: 0, mr: 1, ml: { xs: 0, md: "-26px" } }} />
              <Skeleton variant="text" width={titleWidth} sx={{ maxWidth: 1 }} />
            </Box>
          ) : (
            <Skeleton variant="text" width={titleWidth} sx={{ maxWidth: 1 }} />
          )) as unknown as string
        }
        slots={
          crumbs
            ? {
                breadcrumbs: (
                  <Breadcrumbs separator={<BreadcrumbsSeparator />} sx={{ typography: "body2" }}>
                    {/* The parent crumb is a link: PhoneTapStyles grows `li > a` to 44px below md. */}
                    {crumbLink ? (
                      <Box component="a" aria-hidden="true" tabIndex={-1}>
                        <Skeleton variant="text" width={crumbWidths[0]} />
                      </Box>
                    ) : (
                      <Skeleton variant="text" width={crumbWidths[0]} />
                    )}
                    <Skeleton variant="text" width={crumbWidths[1]} />
                  </Breadcrumbs>
                ),
              }
            : undefined
        }
        action={
          widths.length ? (
            <Box sx={pageHeaderActionsSx(layout)}>
              {widths.map((w, i) => (
                <Skeleton key={i} variant="rounded" sx={{ width: w, maxWidth: 1, height: actionHeights?.[i] ?? tapHeight(36) }} />
              ))}
            </Box>
          ) : undefined
        }
        sx={pageHeaderLayoutSx(layout)}
      />
      {tabs ? <div>{tabs}</div> : null}
      {toolbar ? <div>{toolbar}</div> : null}
    </Box>
  );
}

/** Loading twin of `TemplateTabs`: the template MUI Tabs strip, labels as text Skeletons. */
export function TabsSkeleton({ count, variant = "underline", counts = false, widths, links = false, sx }: { count: number; variant?: "underline" | "pill"; counts?: boolean; /** Per-tab label widths when the page's labels are known (default: a wobble 56-95). */ widths?: number[]; /** The page's tabs are links (SegmentTabs with hrefs): `a` tabs, which the legacy `.main button` min-height rules do not reach. */ links?: boolean; sx?: SxProps<Theme> }) {
  return (
    <Tabs
      value={false}
      variant="scrollable"
      scrollButtons={false}
      aria-hidden="true"
      data-skel="tabs"
      indicatorColor={variant === "pill" ? ("custom" as never) : undefined}
      sx={[variant === "pill" ? { width: "fit-content", maxWidth: "100%", borderRadius: 1 } : {}, ...(Array.isArray(sx) ? sx : [sx])]}
    >
      {Array.from({ length: count }, (_, i) => (
        <Tab
          key={i}
          disabled
          tabIndex={-1}
          {...(links ? { component: "a" as const } : {})}
          label={
            <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
              <Skeleton variant="text" width={widths?.[i] ?? 56 + ((i * 23) % 40)} />
              {counts ? <Skeleton variant="rounded" width={24} height={24} /> : null}
            </Box>
          }
        />
      ))}
    </Tabs>
  );
}

/** One outlined field (select / date / search) at the template TextField height. */
export function FieldSkeleton({ width = 200, grow = false, small = false, height, basis }: { width?: number | string | Record<string, number | string>; grow?: boolean; small?: boolean; height?: number | Record<string, number>; /** A grow field's flex basis when it is not the FilterBar search default. */ basis?: number | Record<string, number> }) {
  return <Skeleton variant="rounded" sx={{ height: height ?? (small ? tapHeight(40) : FIELD_H), width: grow ? "auto" : width, flexGrow: grow ? 1 : 0, flexShrink: 1, flexBasis: grow ? (basis ?? FILTER_SEARCH_BASIS) : "auto", maxWidth: 1, minWidth: 0 }} />;
}

export type FilterField = number | "search" | "chip";

/**
 * Loading twin of the filter toolbars (FilterBar / WorklistFilters): a Card with `p 2.5`, fields
 * wrapping with `gap 2`. `fields` is the page's own field list: a number is a fixed-width field
 * (select / date), "search" takes the remaining width, "chip" is a chip-sized pill.
 * `inCard` drops the Card for a toolbar that sits inside another card.
 */
export function FilterCardSkeleton({
  fields,
  actions = 0,
  actionWidths,
  fold = false,
  foldSearch = false,
  inCard = false,
  small = false,
  bare = false,
  summary = false,
  searchSmall = false,
  searchBasis,
}: {
  /** The search's flex basis when the bar sets its own (FilterBar fold: FILTER_SEARCH_FOLD_BASIS). */
  searchBasis?: number | Record<string, number>;
  fields: FilterField[] | number;
  actions?: number;
  /** Per-action widths (a text button ~96, the ⋮ icon button 36); default 88 each. A breakpoint map for a control group that fills a phone row. */
  actionWidths?: (number | Record<string, number | string>)[];
  /** FilterBar `fold`: below md the fixed fields leave the bar for a "Filters" button (search stays). */
  fold?: boolean;
  /** With `fold`: the search folds too (it sits in the md+ controls box, in field order: HerdSignalsFilters). */
  foldSearch?: boolean;
  inCard?: boolean;
  small?: boolean;
  /** FilterBar `bare`: no card, no padding (the template job-list toolbar on the page). */
  bare?: boolean;
  /**
   * FilterBar `summary` that is always on (a default filter such as the business day shows as a
   * result chip): the `px 2.5, pb 2.5` strip with the count, one chip and Clear all (30px row).
   */
  summary?: boolean;
  /** The search field is the 44px phone field (a fullWidth TextField without a label) while the selects are full height. */
  searchSmall?: boolean;
}) {
  const list: FilterField[] = typeof fields === "number" ? Array.from({ length: fields }, () => 200) : fields;
  const acts = actionWidths ?? Array.from({ length: actions }, () => 88);
  const fixed = list.filter((f) => f !== "search");
  const field = (f: FilterField, i: number) =>
    f === "search" ? (
      <FieldSkeleton key={i} grow small={small || searchSmall} />
    ) : f === "chip" ? (
      <ChipSkeleton key={i} />
    ) : (
      <FieldSkeleton key={i} width={{ xs: "100%", sm: f } as never} small={small} />
    );
  const body = fold ? (
    // FilterBar's own order: the controls Box (md+), the search, the Filters button (< md), actions.
    <Box sx={{ p: bare ? 0 : 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center" }}>
      {foldSearch ? (
        <Box sx={{ display: { xs: "none", md: "flex" }, flexWrap: "wrap", gap: 2, alignItems: "center", flex: "1 1 auto", minWidth: 0 }}>
          {list.map((f, i) => (f === "search" ? <FieldSkeleton key={i} grow small={small} /> : f === "chip" ? <ChipSkeleton key={i} /> : <FieldSkeleton key={i} width={f} small={small} />))}
        </Box>
      ) : fixed.length ? (
        <Box sx={{ display: { xs: "none", md: "flex" }, flexWrap: "wrap", gap: 2, alignItems: "center" }}>{fixed.map((f, i) => (f === "chip" ? <ChipSkeleton key={i} /> : <FieldSkeleton key={i} width={f} small={small} />))}</Box>
      ) : null}
      {!foldSearch && list.includes("search") ? <FieldSkeleton grow small={small} basis={searchBasis} /> : null}
      <Skeleton variant="rounded" width={96} sx={{ height: tapHeight(36), display: { xs: "block", md: "none" } }} />
      {acts.length ? (
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center", ml: "auto" }}>
          {acts.map((w, i) => <Skeleton key={i} variant="rounded" sx={typeof w === "number" ? { width: w <= 40 ? tapHeight(w) : w, height: tapHeight(w <= 40 ? w : 30) } : { width: w, height: tapHeight(30) }} />)}
        </Box>
      ) : null}
    </Box>
  ) : (
    <Box sx={{ p: bare ? 0 : 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center" }}>
      {list.map(field)}
      {acts.map((w, i) => (
        <Skeleton key={`a${i}`} variant="rounded" sx={{ width: w, height: tapHeight(36) }} />
      ))}
    </Box>
  );
  const strip = summary ? (
    <Box sx={{ px: bare ? 0 : 2.5, pt: bare ? 2 : 0, pb: bare ? 0 : 2.5, display: "flex", alignItems: "center", gap: 1 }}>
      <Skeleton variant="text" width={32} />
      <ChipSkeleton width={144} height={24} />
      <Skeleton variant="rounded" width={64} height={30} />
    </Box>
  ) : null;
  if (inCard || bare)
    return (
      <div data-skel="filters">
        {body}
        {strip}
      </div>
    );
  return (
    <Card aria-hidden="true" data-skel="filters" sx={{ overflow: "visible" }}>
      {body}
      {strip}
    </Card>
  );
}

export type KpiShape = {
  /** EcommerceWidgetSummary (KpiWidget trend.period "week" / "7d"): title, h3 figure, trend row, 100x66 sparkline. */
  spark?: boolean;
  /** BookingWidgetSummary (KpiWidget trend.period "month"): title, h3 figure, trend row, 120px round icon. */
  booking?: boolean;
  /** Trending row under the figure (ecommerce / booking only; the course card has none). */
  trend?: boolean;
  /** The KpiWidget `caption` sub-line (body2, mt 1) under the card body. */
  hint?: boolean;
  /** Lines the caption wraps to at the page's card width (a long backend explanation); default 1. Per breakpoint: `{ xs: 1, md: 2 }`. */
  hintLines?: number | Partial<Record<"xs" | "sm" | "md" | "lg" | "xl", number>>;
  /**
   * A text fact tile (not a number widget): Card > CardHeader with a 40px icon avatar, an h6 value
   * and a body2 label, `pb: 3` (the /vaccination/plan live-version facts).
   */
  fact?: boolean;
  /** @deprecated retired KpiCard anatomy; the loaded decks are all KpiWidget. Renders the course card. */
  icon?: boolean;
  /** @deprecated see `icon`. */
  parts?: boolean;
  /** @deprecated see `icon`. */
  hero?: boolean;
};

/**
 * Loading twin of `KpiWidget` (components/app/kpi-widget), card for card: the SAME template card
 * paddings and rows the adapter picks, with text Skeletons at the real typography line heights.
 *  - default: CourseWidgetSummary (py 3, pl 3, pr 2.5; h3 figure, subtitle2 title, 36px corner icon
 *    tile at top 24 / right 20);
 *  - `spark`: EcommerceWidgetSummary (p 3; subtitle2 title, h3 figure my 1.5, trend row, 100x66 chart);
 *  - `booking`: BookingWidgetSummary (p 2, pl 3; the same rows, 120px round icon).
 * `hint` is the adapter's caption sub-line (body2 with mt 1, reserved inside the card).
 */
export function KpiCardSkeleton({ spark, booking, trend, hint, hintLines = 1, fact }: KpiShape) {
  if (fact) {
    return (
      <Card aria-hidden="true" data-skel-kpi="fact" sx={{ height: 1 }}>
        <CardHeader
          avatar={<Skeleton variant="rounded" width={40} height={40} />}
          title={<Skeleton variant="text" width="56%" />}
          subheader={<Skeleton variant="text" width="72%" />}
          slotProps={{ title: { variant: "h6" } }}
          sx={{ pb: 3 }}
        />
      </Card>
    );
  }
  const lines = typeof hintLines === "number" ? { xs: hintLines } : hintLines;
  const most = Math.max(1, ...Object.values(lines).map((n) => n ?? 1));
  // Line i shows at a breakpoint whose caption wraps to more than i lines; the last shown is short.
  const shown = (i: number) => Object.fromEntries(Object.entries(lines).map(([bp, n]) => [bp, i < Math.max(1, n ?? 1) ? "block" : "none"]));
  const lastShort = (i: number) => Object.fromEntries(Object.entries(lines).map(([bp, n]) => [bp, i === Math.max(1, n ?? 1) - 1 ? "72%" : "100%"]));
  const caption = hint ? (
    <Box sx={{ mt: 1, flexBasis: "100%" }}>
      {Array.from({ length: most }, (_, i) => (
        <SkeletonLine key={i} variant="body2" width={lastShort(i) as never} sx={{ display: shown(i) }} />
      ))}
    </Box>
  ) : null;
  const trendRow = (
    <Box sx={{ gap: 0.5, display: "flex", alignItems: "center", height: "var(--sp-3)" }}>
      <Skeleton variant="circular" width={24} height={24} />
      <Skeleton variant="text" width={96} />
    </Box>
  );
  if (spark || booking) {
    return (
      <Card aria-hidden="true" data-skel-kpi={spark ? "ecommerce" : "booking"} sx={{ ...(spark ? { p: 3 } : { p: 2, pl: 3 }), display: "flex", alignItems: "center", flexWrap: "wrap", height: 1 }}>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <SkeletonLine variant="subtitle2" width="56%" />
          <Box sx={{ my: 1.5, typography: "h3" }}>
            <Skeleton variant="text" width="42%" />
          </Box>
          {trend !== false ? trendRow : null}
        </Box>
        {spark ? <Skeleton variant="rounded" width={100} height={66} /> : <Skeleton variant="circular" width={120} height={120} />}
        {caption}
      </Card>
    );
  }
  return (
    <Card aria-hidden="true" data-skel-kpi="course" sx={{ py: 3, pl: 3, pr: 2.5, position: "relative", height: 1 }}>
      <Box sx={{ flexGrow: 1, pr: 6 }}>
        <Box sx={{ typography: "h3" }}>
          <Skeleton variant="text" width="36%" />
        </Box>
        <SkeletonLine variant="subtitle2" width="56%" />
      </Box>
      {caption}
      <Skeleton variant="rounded" width={36} height={36} sx={{ position: "absolute", top: 24, right: 20, borderRadius: 0.75 }} />
    </Card>
  );
}

/**
 * Loading twin of `KpiGrid` + `KpiCard`: the same Grid (count-driven sizes, spacing 3), so the row
 * breaks exactly where the real deck breaks. `shapes` sets per-card anatomy when cards differ.
 */
export function KpiRowSkeleton({
  count,
  shapes,
  size,
  sizes,
  ...shape
}: {
  count: number;
  shapes?: KpiShape[];
  /** The page's own Grid item size when it lays its widgets out itself instead of KpiGrid. */
  size?: Record<string, number | "grow" | "auto">;
  /** Per-tile Grid sizes (a 3 + 2 row pair where each row fills its width). */
  sizes?: Record<string, number | "grow" | "auto">[];
} & KpiShape) {
  const cards = Array.from({ length: count }, (_, i) => <KpiCardSkeleton key={i} {...(shapes?.[i] ?? shape)} />);
  return (
    <div data-skel="kpis" aria-hidden="true">
      {size || sizes ? (
        <Grid container spacing={3}>
          {cards.map((card, i) => (
            <Grid key={i} size={(sizes?.[i] ?? size) as never} sx={{ minWidth: 0 }}>
              {card}
            </Grid>
          ))}
        </Grid>
      ) : (
        <KpiGrid>{cards}</KpiGrid>
      )}
    </div>
  );
}

/**
 * Loading twin of `StatStrip` in its Card (template InvoiceAnalytic row): 56px ring, subtitle1 label,
 * optional body2 meta, subtitle2 figure; one row of dashed-divided cells up to four, rows of three or
 * four after that (the same `stripColumns` rule).
 */
export function StatStripSkeleton({ count, meta = false, card = true, wrapBelowMd = false, minHeight = true }: { count: number; meta?: boolean; card?: boolean; /** The page's strip Scrollbar keeps its min height (false: the strip is its cells' height). */ minHeight?: boolean; /** The cells' title and meta lines wrap to two lines each on a phone (200px cells, long copy); "title" when only the title wraps. */ wrapBelowMd?: boolean | "title" }) {
  const cols = count <= 4 ? Math.max(count, 1) : count % 3 === 0 && count % 4 !== 0 ? 3 : 4;
  const cell = (i: number) => (
    <Box key={i} sx={{ width: 1, minWidth: 200, gap: 2.5, px: 2, display: "flex", alignItems: "center", justifyContent: "center" }}>
      <Skeleton variant="circular" width={56} height={56} sx={{ flexShrink: 0 }} />
      <Box sx={{ minWidth: 0, flex: "0 1 96px" }}>
        <SkeletonLine variant="subtitle1" width="100%" />
        {wrapBelowMd ? <SkeletonLine variant="subtitle1" width="60%" sx={{ display: { md: "none" } }} /> : null}
        {meta ? (
          <Box sx={{ my: 0.5 }}>
            <SkeletonLine variant="body2" width="80%" />
            {wrapBelowMd === true ? <SkeletonLine variant="body2" width="50%" sx={{ display: { md: "none" } }} /> : null}
          </Box>
        ) : null}
        <SkeletonLine variant="subtitle2" width="56%" />
      </Box>
    </Box>
  );
  const strip =
    count > cols ? (
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "minmax(0,1fr)", md: `repeat(${cols}, minmax(0,1fr))` }, "& > *": { py: 2 } }}>
        {Array.from({ length: count }, (_, i) => cell(i))}
      </Box>
    ) : (
      <Box sx={{ minHeight: minHeight ? "calc(var(--sp-6) * 2.25)" : 0, overflow: "hidden" }}>
        <Stack direction="row" divider={<Divider orientation="vertical" flexItem sx={{ borderStyle: "dashed" }} />} sx={{ py: 2 }}>
          {Array.from({ length: count }, (_, i) => cell(i))}
        </Stack>
      </Box>
    );
  if (!card) return <div data-skel="stats">{strip}</div>;
  return (
    <Card aria-hidden="true" data-skel="stats">
      {strip}
    </Card>
  );
}

/** Card header twin: CardHeader with an h6 title and optional body2 subheader / action. */
export function CardHeaderSkeleton({ subheader = false, action = false, sx }: { subheader?: boolean; action?: boolean | ReactNode; sx?: SxProps<Theme> }) {
  const actionSx = action && action !== true ? { alignItems: "center", flexWrap: "wrap", rowGap: 1.5, "& .MuiCardHeader-action": { m: 0, minWidth: 0, maxWidth: 1 } } : {};
  return (
    <CardHeader
      title={<Skeleton variant="text" width="32%" />}
      subheader={subheader ? <Skeleton variant="text" width="48%" /> : undefined}
      action={action === true ? <Skeleton variant="rounded" width={96} height={36} /> : action || undefined}
      sx={[actionSx, ...(Array.isArray(sx) ? sx : [sx])]}
    />
  );
}

/**
 * Loading twin of a stat-grid card (template invoice-analytic cells in an outlined dashed Paper, the pen
 * detail's Work state card): CardHeader, then `cells` cells of `p 2.5` (body2 caption + h4 figure) in
 * `columns` columns per breakpoint, with the same dashed cell dividers.
 */
export function StatGridCardSkeleton({ cells, columns }: { cells: number; columns: Record<string, number> }) {
  const gridTemplateColumns = Object.fromEntries(Object.entries(columns).map(([bp, n]) => [bp, `repeat(${n}, minmax(0, 1fr))`]));
  return (
    <Card aria-hidden="true" data-skel="stat-grid">
      <CardHeaderSkeleton />
      <Paper variant="outlined" sx={{ m: 3, borderStyle: "dashed", overflow: "hidden" }}>
        <Box sx={{ mr: "-1px", mb: "-1px", display: "grid", gridTemplateColumns, "& > *": { borderRight: 1, borderBottom: 1, borderColor: "divider", borderStyle: "dashed" } }}>
          {Array.from({ length: cells }, (_, i) => (
            <Box key={i} sx={{ p: 2.5, minWidth: 0 }}>
              <SkeletonLine variant="body2" width={wobble(i)} sx={{ mb: 0.5 }} />
              <SkeletonLine variant="h4" width={32} />
            </Box>
          ))}
        </Box>
      </Paper>
    </Card>
  );
}

/** Loading twin of a chart card: Card + CardHeader + the chart's own plot height. */
export function ChartCardSkeleton({ height = 300, subheader = false, action = false, legend = false }: { height?: number | Record<string, number>; subheader?: boolean; action?: boolean | ReactNode; legend?: boolean }) {
  return (
    <Card aria-hidden="true" data-skel="chart">
      <CardHeaderSkeleton subheader={subheader} action={action} />
      <Box sx={{ p: 3, pt: 2 }}>
        {legend ? (
          <Box sx={{ display: "flex", gap: 2, flexWrap: "wrap", mb: 2 }}>
            {[64, 80, 72].map((w, i) => (
              <Box key={i} sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Skeleton variant="circular" width={12} height={12} />
                <Skeleton variant="text" width={w} />
              </Box>
            ))}
          </Box>
        ) : null}
        <Skeleton variant="rounded" sx={{ height }} />
      </Box>
    </Card>
  );
}

/** Rows as a placeholder table body: the page's column count, `rows` per page. */
function SkeletonTableBody({ columns, rows, dense, lead }: { columns: number; rows: number; dense?: boolean; lead?: "avatar" | "two-line" }) {
  return (
    <Box sx={{ overflow: "hidden", minWidth: 0 }}>
      <Table size={dense ? "small" : "medium"} sx={{ tableLayout: "fixed", width: 1, minWidth: "0 !important" }}>
        <TableHead>
          <TableRow>
            {Array.from({ length: columns }, (_, c) => (
              <TableCell key={c} component="th">
                <Skeleton variant="text" width={c === 0 ? "50%" : "64%"} />
              </TableCell>
            ))}
          </TableRow>
        </TableHead>
        <TableBody>
          {Array.from({ length: rows }, (_, r) => (
            <TableRow key={r}>
              {Array.from({ length: columns }, (_, c) => (
                <TableCell key={c}>
                  {c === 0 && lead === "avatar" ? (
                    // Template user row lead: 40px avatar, then the name (subtitle2) over a caption line.
                    <Box sx={{ display: "flex", alignItems: "center", gap: 2, minWidth: 0 }}>
                      <Skeleton variant="circular" width={40} height={40} sx={{ flexShrink: 0 }} />
                      <Box sx={{ flex: 1, minWidth: 0 }}>
                        <SkeletonLine variant="subtitle2" width="70%" />
                        <SkeletonLine variant="body2" width="90%" />
                      </Box>
                    </Box>
                  ) : c === 0 && lead === "two-line" ? (
                    <Box sx={{ minWidth: 0 }}>
                      <SkeletonLine variant="subtitle2" width="76%" />
                      <SkeletonLine variant="body2" width="52%" />
                    </Box>
                  ) : (
                    <Skeleton variant="text" width={c === 0 ? "76%" : wobble(r * 3 + c)} />
                  )}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Box>
  );
}

/** Pager twin: the template TablePaginationCustom 64px toolbar, range + arrows right-aligned. */
export function PagerSkeleton() {
  return (
    <Box aria-hidden="true" sx={{ minHeight: "calc(var(--sp-4) * 2)", px: 2, display: "flex", alignItems: "center", justifyContent: "flex-end", columnGap: 3 }}>
      <Skeleton variant="text" width={120} sx={{ display: { xs: "none", sm: "block" } }} />
      <Skeleton variant="text" width={72} />
      <Box sx={{ display: "flex", gap: 1 }}>
        <Skeleton variant="circular" width={32} height={32} />
        <Skeleton variant="circular" width={32} height={32} />
      </Box>
    </Box>
  );
}

/**
 * Loading twin of a table card (template user-list anatomy): Card > optional CardHeader > optional
 * tab strip > optional toolbar > Table (the page's real column count, `rows` = rows per page) >
 * optional pager. `bare` renders it without the Card for a table inside another card.
 */
export function TableSkeleton({
  columns,
  rows = 10,
  header = true,
  subheader = false,
  headerAction = false,
  headerSx,
  tabs,
  toolbar,
  pager = true,
  dense = false,
  bare = false,
  hideBelow,
  lead,
  id,
  children,
}: {
  /** Not drawn below this breakpoint (the page shows a stacked phone list there: StackedRowsSkeleton). */
  hideBelow?: "sm" | "md";
  /** The first column's shape: "avatar" = the template user row (40px avatar + two lines); "two-line" = a name over a caption line (a pen over its park). Rows then have the loaded rows' height. */
  lead?: "avatar" | "two-line";
  /** The section's anchor id, so a hash link lands on the placeholder while the section streams. */
  id?: string;
  columns: number;
  rows?: number;
  header?: boolean;
  subheader?: boolean;
  headerAction?: boolean | ReactNode;
  /** The page CardHeader's own padding when it overrides the theme's (e.g. `{ px: 3, pt: 2.5, pb: 1.5 }`). */
  headerSx?: SxProps<Theme>;
  tabs?: ReactNode;
  toolbar?: ReactNode;
  pager?: boolean;
  dense?: boolean;
  bare?: boolean;
  /** Extra rows between the toolbar and the table (a totals strip, a banner). */
  children?: ReactNode;
}) {
  const body = (
    <>
      {header ? <CardHeaderSkeleton subheader={subheader} action={headerAction} sx={headerSx} /> : null}
      {tabs ? <Box sx={{ px: 2.5, pt: header ? 2 : 0 }}>{tabs}</Box> : null}
      {toolbar}
      {children}
      <Box sx={{ pt: header && !toolbar && !tabs && !children ? 3 : 0 }}>
        <SkeletonTableBody columns={columns} rows={rows} dense={dense} lead={lead} />
      </Box>
      {pager ? <PagerSkeleton /> : null}
    </>
  );
  const display = hideBelow ? { xs: "none", [hideBelow]: "block" } : undefined;
  if (bare) return <Box id={id} data-skel="table" sx={display ? { display } : undefined}>{body}</Box>;
  return (
    <Card id={id} aria-hidden="true" data-skel="table" sx={{ overflow: "hidden", ...(display ? { display } : {}) }}>
      {body}
    </Card>
  );
}

/**
 * Loading twin of a card list (template sections/job job-list + job-item): a CSS grid with gap 3 and
 * `columns` per breakpoint; each card is 48px rounded avatar, subtitle1 title, caption meta, a Label,
 * then a dashed divider over a two-column grid of caption facts.
 */
export function CardGridSkeleton({
  count,
  columns = { xs: 1, sm: 2, md: 3 },
  facts = 4,
  label = true,
  optionalFrom,
}: {
  count: number;
  /** Cards from this index on stand for cards the page draws only when it has that many (the SOP
   *  libraries hold 1-4): they carry data-skel-optional, so a 1-card library is not "extra" (TR3-P0-3). */
  optionalFrom?: number;
  columns?: { xs?: number; sm?: number; md?: number; lg?: number; xl?: number };
  facts?: number;
  label?: boolean;
}) {
  const template = Object.fromEntries(Object.entries(columns).map(([bp, n]) => [bp, `repeat(${n}, 1fr)`]));
  return (
    <Box aria-hidden="true" data-skel="cards" sx={{ gap: 3, display: "grid", gridTemplateColumns: template }}>
      {Array.from({ length: count }, (_, i) => (
        <Card key={i} {...(optionalFrom != null && i >= optionalFrom ? { "data-skel-optional": "" } : {})}>
          <Box sx={{ p: 3, pb: 2 }}>
            <Skeleton variant="rounded" width={48} height={48} sx={{ mb: 2 }} />
            <SkeletonLine variant="subtitle1" width={wobble(i, 50, 30)} />
            <SkeletonLine variant="caption" width="36%" sx={{ mt: 1, mb: 1 }} />
            {label ? <Skeleton variant="rounded" width={64} height={24} /> : null}
          </Box>
          {facts ? (
            <>
              <Divider sx={{ borderStyle: "dashed" }} />
              <Box sx={{ p: 3, rowGap: 1.5, columnGap: 1, display: "grid", gridTemplateColumns: "repeat(2, 1fr)" }}>
                {Array.from({ length: facts }, (_, f) => (
                  <SkeletonLine key={f} variant="caption" width={wobble(i + f, 50, 30)} />
                ))}
              </Box>
            </>
          ) : null}
        </Card>
      ))}
    </Box>
  );
}

/** Loading twin of the centred MUI Pagination under a card list (template job-list). */
export function PaginationSkeleton({ pages = 5 }: { pages?: number }) {
  return (
    <Box aria-hidden="true" data-skel="pagination" sx={{ mt: { xs: 5, md: 8 }, display: "flex", justifyContent: "center", gap: 1 }}>
      {Array.from({ length: pages + 2 }, (_, i) => (
        <Skeleton key={i} variant="circular" width={32} height={32} />
      ))}
    </Box>
  );
}

/** Loading twin of a detail / form card: CardHeader then label-value rows (`columns` side by side). */
export function DetailCardSkeleton({ rows = 5, columns = 1, header = true, height }: { rows?: number; columns?: number; header?: boolean; /** A fixed height, or "100%" per breakpoint where the page stretches the card to its Grid row. */ height?: number | string | Record<string, number | string> }) {
  return (
    <Card aria-hidden="true" data-skel="detail" sx={{ ...(height ? { height } : {}) }}>
      {header ? <CardHeaderSkeleton /> : null}
      <Box sx={{ p: 3, display: "grid", gap: { xs: 0, md: 4 }, gridTemplateColumns: { xs: "1fr", md: `repeat(${columns}, minmax(0, 1fr))` } }}>
        {Array.from({ length: columns }, (_, c) => (
          <Stack key={c} spacing={1.5}>
            {Array.from({ length: rows }, (_, r) => (
              <Box key={r} sx={{ display: "grid", gridTemplateColumns: "38% 1fr", gap: 2, alignItems: "center" }}>
                <SkeletonLine variant="body2" width="70%" />
                <SkeletonLine variant="subtitle2" width={wobble(r + c * 5)} />
              </Box>
            ))}
          </Stack>
        ))}
      </Box>
    </Card>
  );
}

/**
 * Loading twin of a small settings card (the /leave "who approves" card): Card `p { xs 2, sm 3 }`, a
 * bare CardHeader (`p 0, mb 2`; h6 title, body2 subheader that wraps to a second line on a phone when
 * `wrap`), then one wrapping row (gap 2.5) of controls (`controls` = their widths: a checkbox label is
 * the box + its label) and an optional small button.
 */
export function FormCardSkeleton({ subheader = true, wrap = false, controls = [], action }: { subheader?: boolean; wrap?: boolean; controls?: number[]; action?: number }) {
  return (
    <Card aria-hidden="true" data-skel="form" sx={{ p: { xs: 2, sm: 3 } }}>
      <Box sx={{ mb: 2 }}>
        <SkeletonLine variant="h6" width="32%" />
        {subheader ? <SkeletonLine variant="body2" width="72%" /> : null}
        {subheader && wrap ? <SkeletonLine variant="body2" width="48%" sx={{ display: { xs: "block", sm: "none" } }} /> : null}
      </Box>
      <Box sx={{ display: "flex", gap: 2.5, flexWrap: "wrap", alignItems: "center" }}>
        {controls.map((w, i) => (
          <Skeleton key={i} variant="rounded" width={w} sx={{ height: tapHeight(36) }} />
        ))}
        {action ? <Skeleton variant="rounded" width={action} sx={{ height: tapHeight(30) }} /> : null}
      </Box>
    </Card>
  );
}

/**
 * Loading twin of a template mail-nav rail (MailNavItem rows under overline subheaders, in a Card with
 * `py 1.5`): per group an overline row, then its items at the 44px tap height; below md the items wrap
 * into rows as the page's rail does. `groups` = items per group.
 */
export function NavRailSkeleton({ groups }: { groups: number[] }) {
  return (
    <Card aria-hidden="true" data-skel="rail" sx={{ py: 1.5 }}>
      {groups.map((items, g) => (
        <Box key={g} sx={{ px: 1.5, pb: 1, display: { xs: "flex", md: "block" }, flexWrap: "wrap", columnGap: 0.5 }}>
          <Box sx={{ width: 1, px: 1, pt: 1.5, pb: 1 }}>
            <SkeletonLine variant="caption" width={96} />
          </Box>
          {Array.from({ length: items }, (_, i) => (
            <Box key={i} sx={{ height: TAP_MIN, display: "flex", alignItems: "center", px: 1, width: { xs: 82 + ((i * 37) % 60), md: 1 } }}>
              <SkeletonLine variant="body2" width={`${60 + ((i * 17) % 30)}%`} />
            </Box>
          ))}
        </Box>
      ))}
    </Card>
  );
}

/** Avatar + two-line rows (a notification list, a people list) with a trailing value; no card. */
export function ListRowsSkeleton({ rows = 5, avatar = true, trailing = true, spacing = 2 }: { rows?: number; avatar?: boolean; trailing?: boolean; spacing?: number }) {
  return (
    <Stack spacing={spacing} aria-hidden="true" data-skel="rows">
      {Array.from({ length: rows }, (_, i) => (
        <Box key={i} sx={{ display: "flex", alignItems: "center", gap: 2 }}>
          {avatar ? <Skeleton variant="circular" width={40} height={40} sx={{ flexShrink: 0 }} /> : null}
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <SkeletonLine variant="subtitle2" width={wobble(i, 50, 30)} />
            <SkeletonLine variant="body2" width={wobble(i + 2, 28, 22)} />
          </Box>
          {trailing ? <Skeleton variant="text" width={56} /> : null}
        </Box>
      ))}
    </Stack>
  );
}

/**
 * Phone twin of a list that stacks below sm (one divider row per record instead of a table clipped
 * at the card edge, e.g. the /people directory): each row is a subtitle2 name with an optional soft
 * Label beside it, `lines` body2 lines, an optional Label line, and an optional 44px trailing action,
 * on the page's own row padding (pl 2, pr 1, py 1.25, divider below). Shown only below `sm` unless
 * `always`, so one fallback can pair it with the table twin from sm (FIXJ11, J3B N-P1-2: the /people
 * pager showed a 6-column desktop table skeleton on phones and dropped the reader 1150px).
 */
export function StackedRowsSkeleton({
  rows,
  headLabel = false,
  lines = 1,
  tailLabel = false,
  trailing = false,
  always = false,
}: {
  rows: number;
  /** A soft Label beside the name (a status). */
  headLabel?: boolean;
  /** body2 lines under the name. */
  lines?: number;
  /** A soft Label line last (a clock-in chip). */
  tailLabel?: boolean;
  /** A 44px trailing IconButton (row action). */
  trailing?: boolean;
  always?: boolean;
}) {
  return (
    <Box aria-hidden="true" data-skel="rows" sx={{ display: always ? "block" : { xs: "block", sm: "none" }, borderTop: 1, borderColor: "divider" }}>
      {Array.from({ length: rows }, (_, i) => (
        <Box key={i} sx={{ display: "flex", alignItems: "center", gap: 1, pl: 2, pr: 1, py: 1.25, borderBottom: 1, borderColor: "divider" }}>
          <Stack spacing={0.5} sx={{ flex: "1 1 auto", minWidth: 0, minHeight: "var(--tap-min)", justifyContent: "center" }}>
            <Stack direction="row" spacing={1} sx={{ alignItems: "center", minWidth: 0 }}>
              <SkeletonLine variant="subtitle2" width={wobble(i, 34, 24)} />
              {headLabel ? <Skeleton variant="rounded" width={56} height={24} sx={{ flexShrink: 0 }} /> : null}
            </Stack>
            {Array.from({ length: lines }, (_, l) => (
              <SkeletonLine key={l} variant="body2" width={wobble(i + l + 1, 52, 30)} />
            ))}
            {tailLabel ? <Skeleton variant="rounded" width={112} height={24} /> : null}
          </Stack>
          {trailing ? (
            <Box sx={{ width: "var(--tap-min)", height: "var(--tap-min)", flexShrink: 0, display: "grid", placeItems: "center" }}>
              <Skeleton variant="circular" width={20} height={20} />
            </Box>
          ) : null}
        </Box>
      ))}
    </Box>
  );
}

/** Loading twin of a list card: CardHeader then avatar + two-line rows. */
export function ListCardSkeleton({ rows = 5, avatar = true, header = true }: { rows?: number; avatar?: boolean; header?: boolean }) {
  return (
    <Card aria-hidden="true" data-skel="list">
      {header ? <CardHeaderSkeleton /> : null}
      <Box sx={{ p: 3 }}>
        <ListRowsSkeleton rows={rows} avatar={avatar} />
      </Box>
    </Card>
  );
}

/** A block of fixed height (a full calendar, an alert band) as one rounded Skeleton in a Card. */
export function BlockSkeleton({ height, card = true }: { height: number | string | Record<string, number | string>; card?: boolean }) {
  if (!card) return <Skeleton aria-hidden="true" data-skel="block" variant="rounded" sx={{ height }} />;
  return (
    <Card aria-hidden="true" data-skel="block" sx={{ height, p: 3 }}>
      <Skeleton variant="rounded" sx={{ height: 1 }} />
    </Card>
  );
}

/**
 * A page toolbar row that is not a card: a tab strip / chip row on the left, filter fields on the
 * right, wrapping under each other on a phone (the approvals / adherence / feed-tabbar rows).
 */
export function ToolbarSkeleton({ left, fields = [], small = true, searchHeight, searchWidth, card = false, sx }: { left?: ReactNode; fields?: FilterField[]; small?: boolean; /** The search field's own height per breakpoint (a 44px phone field that is the 56px template field from md). */ searchHeight?: Record<string, number>; /** A fixed search width per breakpoint instead of a growing search (a full phone row, 294 from md). */ searchWidth?: Record<string, number | string>; /** The toolbar is its own Card (`p 2.5`): the Action Center board toolbar card. */ card?: boolean; sx?: SxProps<Theme> }) {
  const row = (
    <Box aria-hidden="true" data-skel="toolbar" sx={[{ display: "flex", flexWrap: "wrap", alignItems: "center", justifyContent: "space-between", gap: 2, minWidth: 0 }, card ? { p: 2.5 } : {}, ...(Array.isArray(sx) ? sx : [sx])]}>
      {left ? <Box sx={{ minWidth: 0, maxWidth: 1 }}>{left}</Box> : null}
      {fields.length ? (
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 2, alignItems: "center", minWidth: 0, ...(card ? { flex: { xs: "1 1 100%", md: "0 0 auto" } } : {}) }}>
          {fields.map((f, i) =>
            f === "search" ? (searchWidth ? <FieldSkeleton key={i} width={searchWidth} small={small} height={searchHeight} /> : <FieldSkeleton key={i} grow small={small} height={searchHeight} />) : f === "chip" ? <ChipSkeleton key={i} /> : <FieldSkeleton key={i} width={f} small={small} />,
          )}
        </Box>
      ) : null}
    </Box>
  );
  return card ? <Card aria-hidden="true" data-skel="toolbar-card">{row}</Card> : row;
}

/**
 * A page's own wrapping control row that is not a card (a date field + a segment strip + a caption
 * line): `Stack direction="row" flexWrap gap 2`, the caption on its own line under the controls.
 */
export function ControlRowSkeleton({ children, caption }: { children: ReactNode; caption?: number }) {
  return (
    <Box aria-hidden="true" data-skel="controls-row" sx={{ display: "flex", flexWrap: "wrap", gap: 2, alignItems: "center", minWidth: 0 }}>
      {children}
      {/* the caption wraps to its own line under the controls at its own width */}
      {caption ? <SkeletonLine variant="caption" width={caption} sx={{ flexShrink: 0, mr: `calc(100% - ${caption}px)` }} /> : null}
    </Box>
  );
}

/** One chip-sized pill (FilterChip / Label). */
export function ChipSkeleton({ width = 88, height = 32 }: { width?: number; height?: number | Record<string, number> }) {
  return <Skeleton variant="rounded" width={width} sx={{ height, borderRadius: 1, flexShrink: 0 }} />;
}

/** A row of chips (FilterChip rows, severity chips). */
export function ChipRowSkeleton({ count, widths, height }: { count: number; widths?: number[]; /** A clickable chip row is 44px tall below sm (tap floor): `{ xs: 44, sm: 32 }`. */ height?: number | Record<string, number> }) {
  return (
    <Box aria-hidden="true" data-skel="chips" sx={{ display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center" }}>
      {Array.from({ length: count }, (_, i) => (
        <ChipSkeleton key={i} width={widths?.[i] ?? 72 + ((i * 29) % 40)} height={height} />
      ))}
    </Box>
  );
}

/** A section heading line (the h6 over a KPI deck / breakdown group). */
export function HeadingSkeleton({ variant = "h6", width = 180 }: { variant?: Typo; width?: number | string }) {
  return (
    <Box aria-hidden="true" data-skel="heading">
      <SkeletonLine variant={variant} width={width} />
    </Box>
  );
}

/** The page's own `Stack spacing={3}` wrapper when its blocks live inside one (leave, routines, ceo-ai-admin). */
export function StackSkeleton({ children, spacing = 3 }: { children: ReactNode; spacing?: number }) {
  return (
    <Stack spacing={spacing} aria-hidden="true" data-skel="stack" sx={{ minWidth: 0 }}>
      {children}
    </Stack>
  );
}

/** The page's own MUI `Grid container spacing={3}`: each child with its Grid `size`. */
export function GridSkeleton({ items, spacing = 3, fill = false }: { items: { size: number | Record<string, number>; node: ReactNode }[]; spacing?: number; /** The page's nested Grid container fills its parent Grid item (`sx={{ height: 1 }}`). */ fill?: boolean }) {
  return (
    // The Grid container itself, no wrapper: the twin nests exactly as deep as the page's Grid.
    <Grid container spacing={spacing} aria-hidden="true" data-skel="grid" sx={fill ? { height: 1 } : undefined}>
      {items.map((item, i) => (
        <Grid key={i} size={item.size as never} sx={{ minWidth: 0 }}>
          {item.node}
        </Grid>
      ))}
    </Grid>
  );
}

/**
 * Loading twin of a lane board. `layout="kanban"` is the template KanbanBoard (336px lanes, 24px gap,
 * neutral lane paper, 16px item gap — the work board); `layout="grid"` is an equal-column board that
 * fills the width (action center / tasks boards), collapsing to 2 then 1 column like theirs.
 */
export function KanbanSkeleton({
  lanes,
  layout = "kanban",
  minHeight,
  laneWidth,
}: {
  lanes: number[];
  layout?: "kanban" | "grid";
  minHeight?: number;
  /** The board's own `--kanban-column-width` when it sets one (the /tasks board fills four lanes, 86vw on a phone). */
  laneWidth?: string | Record<string, string>;
}) {
  const lane = (cards: number, i: number) => (
    <Box
      key={i}
      component="section"
      sx={{
        flexShrink: 0,
        display: "flex",
        flexDirection: "column",
        gap: 2,
        borderRadius: 2,
        bgcolor: "background.neutral",
        minWidth: 0,
        ...(layout === "kanban" ? { width: laneWidth ? capLane(laneWidth) : "min(calc(var(--sp-6) * 7), calc(100vw - var(--sp-6)))" } : {}),
        ...(minHeight ? { minHeight } : {}),
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, pt: 2.5, px: 2 }}>
        <Skeleton variant="circular" width={24} height={24} />
        <SkeletonLine variant="h6" width="46%" />
      </Box>
      <Stack spacing={2} sx={{ px: 2, pb: 2, minHeight: "calc(var(--sp-5) * 2)" }}>
        {Array.from({ length: cards }, (_, c) => (
          <Card key={c} sx={{ p: 2.5, borderRadius: 1.5, boxShadow: "none" }}>
            <SkeletonLine variant="subtitle2" width={wobble(i + c, 56, 30)} />
            <SkeletonLine variant="caption" width={wobble(i + c + 3, 36, 30)} sx={{ mt: 1 }} />
            <Box sx={{ mt: 2, display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <Skeleton variant="text" width={64} />
              <Skeleton variant="circular" width={24} height={24} />
            </Box>
          </Card>
        ))}
      </Stack>
    </Box>
  );
  return (
    <Box
      aria-hidden="true"
      data-skel="board"
      sx={
        layout === "kanban"
          ? { pb: 2, columnGap: 3, display: "flex", alignItems: "flex-start", overflow: "hidden", maxWidth: 1 }
          : { display: "grid", gap: 1.5, alignItems: "start", gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))", lg: `repeat(${lanes.length}, minmax(0, 1fr))` } }
      }
    >
      {lanes.map((cards, i) => lane(cards, i))}
    </Box>
  );
}

/** The template ColumnRoot width rule: the board's column width, never wider than the phone less its gutters. */
function capLane(width: string | Record<string, string>) {
  const cap = (v: string) => `min(${v}, calc(100vw - var(--sp-3) * 2))`;
  return typeof width === "string" ? cap(width) : Object.fromEntries(Object.entries(width).map(([bp, v]) => [bp, cap(v)]));
}

/**
 * A block the page renders only when its data exists (a KPI deck hidden at all-zero, a card shown
 * only with rows). Layout-transparent; the skeleton/loaded layout check may drop it when the page
 * skipped it.
 */
export function OptionalSkeleton({ children }: { children: ReactNode }) {
  return (
    <Box data-skel-optional="" sx={{ display: "contents" }}>
      {children}
    </Box>
  );
}

/**
 * Loading twin of `OrderTableToolbar` (components/app/sections/order): `filters` select fields on
 * `orderToolbarFilterSx` (full width on a phone, `0 1 160px` from md), the `trailing` buttons at the
 * input height, then the search (`orderToolbarSearchSx`) and the ⋮ menu; a column below md, aligned
 * right, as the template order toolbar is.
 */
export function OrderToolbarSkeleton({
  filters = 0,
  fields = [],
  buttons = [],
  trailing = [],
  trailingTall = false,
  search = true,
  menu = false,
}: {
  /** Template selects on `orderToolbarFilterSx`. */
  filters?: number;
  /** Fixed-width fields (md width in px, full width on a phone), after the selects. */
  fields?: number[];
  /** Default-size buttons inside the filter group (a form's Apply). */
  buttons?: number[];
  /** The toolbar's `trailing` buttons; `trailingTall` when they are size large at the input height. */
  trailing?: number[];
  trailingTall?: boolean;
  search?: boolean;
  menu?: boolean;
}) {
  return (
    <Box
      aria-hidden="true"
      data-skel="toolbar"
      sx={{ p: 2.5, gap: 2, display: "flex", pr: { xs: 2.5, md: 1 }, flexDirection: { xs: "column", md: "row" }, alignItems: { xs: "flex-end", md: "center" } }}
    >
      {Array.from({ length: filters }, (_, i) => (
        <Skeleton key={i} variant="rounded" sx={{ ...orderToolbarFilterSx, height: FIELD_H }} />
      ))}
      {fields.map((w, i) => (
        <Skeleton key={`f${i}`} variant="rounded" sx={{ height: FIELD_H, width: { xs: 1, md: w }, flexShrink: 0 }} />
      ))}
      {[...buttons, ...(trailingTall ? [] : trailing)].map((w, i) => (
        <Skeleton key={`b${i}`} variant="rounded" sx={{ height: tapHeight(36), width: w, flexShrink: 0 }} />
      ))}
      {(trailingTall ? trailing : []).map((w, i) => (
        <Skeleton key={`t${i}`} variant="rounded" sx={{ height: "var(--input-h)", width: w, flexShrink: 0 }} />
      ))}
      <Box sx={{ gap: 2, width: 1, flexGrow: 1, display: "flex", alignItems: "center" }}>
        {search ? <Skeleton variant="rounded" sx={{ ...orderToolbarSearchSx, height: FIELD_H }} /> : <Box sx={{ flexGrow: 1 }} />}
        {menu ? <Skeleton variant="circular" sx={{ width: tapHeight(36), height: tapHeight(36), flexShrink: 0 }} /> : null}
      </Box>
    </Box>
  );
}

/**
 * Loading twin of a Card that is a CardHeader (h6 title, a body2 subheader paragraph of
 * `subheaderLines` lines per breakpoint) over a toolbar row (`toolbar`, e.g. OrderToolbarSkeleton):
 * the /ceo-ai-admin lookup card.
 */
export function ToolbarCardSkeleton({ subheaderLines, toolbar }: { subheaderLines: { xs: number; md: number }; toolbar: ReactNode }) {
  const max = Math.max(subheaderLines.xs, subheaderLines.md);
  return (
    <Card aria-hidden="true" data-skel="toolbar-card">
      <CardHeader
        title={<Skeleton variant="text" width="56%" />}
        subheader={
          <>
            {Array.from({ length: max }, (_, i) => (
              <SkeletonLine
                key={i}
                variant="body2"
                width={i === max - 1 ? "70%" : wobble(i, 90, 8)}
                sx={{ display: { xs: i < subheaderLines.xs ? "block" : "none", md: i < subheaderLines.md ? "block" : "none" } }}
              />
            ))}
          </>
        }
      />
      {toolbar}
    </Card>
  );
}

/**
 * Loading twin of `OrderDetailsToolbar` (components/app/sections/order, the template order-details
 * toolbar): the back arrow, the h4 title with its status Label, the body2 date line, and the
 * right-aligned actions (`actionWidths`); a column below md.
 */
export function OrderDetailsToolbarSkeleton({
  back = true,
  status = true,
  titleWidth = 220,
  titleLines = 1,
  subtitleWidth = 180,
  subtitleLines = 1,
  actionWidths = [],
  actions,
  wrapActions = false,
  flush = false,
}: {
  back?: boolean;
  status?: boolean;
  titleWidth?: number;
  /** Lines the h4 title wraps to (per breakpoint), e.g. `{ xs: 2, md: 1 }`. */
  titleLines?: number | Partial<Record<"xs" | "sm" | "md" | "lg" | "xl", number>>;
  subtitleWidth?: number | string;
  /** Lines the body2 subtitle wraps to (per breakpoint); more than one fills the column width. */
  subtitleLines?: number | Partial<Record<"xs" | "sm" | "md" | "lg" | "xl", number>>;
  /** Button actions at the tap height (36 from md). */
  actionWidths?: number[];
  /** Mixed actions: soft Labels (`label`, 24 tall) and buttons; overrides `actionWidths`. */
  actions?: readonly { width: number; label?: boolean }[];
  /** The page's `slotProps.actions` wrap (a phone wraps the chips, left aligned). */
  wrapActions?: boolean;
  /** No bottom margin: on a `.screen` page frame.css zeroes the toolbar's `mb` and the page gap spaces it. */
  flush?: boolean;
}) {
  const lines = (spec: number | Partial<Record<string, number>>) => {
    const perBp = typeof spec === "number" ? { xs: spec } : spec;
    const most = Math.max(1, ...Object.values(perBp).map((n) => n ?? 1));
    const display = (i: number) => Object.fromEntries(Object.entries(perBp).map(([bp, n]) => [bp, i < Math.max(1, n ?? 1) ? "block" : "none"]));
    return { most, display };
  };
  const head = lines(titleLines);
  const sub = lines(subtitleLines);
  const acts: readonly { width: number; label?: boolean }[] = actions ?? actionWidths.map((width) => ({ width }));
  return (
    <Box aria-hidden="true" data-skel="header" sx={{ gap: 3, display: "flex", ...(flush ? {} : { mb: { xs: 3, md: 5 } }), flexDirection: { xs: "column", md: "row" } }}>
      <Box sx={{ gap: 1, display: "flex", alignItems: "flex-start" }}>
        {back ? <Skeleton variant="circular" sx={{ width: tapHeight(36), height: tapHeight(36), flexShrink: 0 }} /> : null}
        <Stack spacing={0.5} sx={{ minWidth: 0 }}>
          <Box sx={{ gap: 1, display: "flex", alignItems: "center" }}>
            <Box sx={{ typography: "h4", width: titleWidth, maxWidth: 1 }}>
              {Array.from({ length: head.most }, (_, i) => (
                <Skeleton key={i} variant="text" sx={i ? { display: head.display(i) } : undefined} />
              ))}
            </Box>
            {status ? <ChipSkeleton width={96} height={24} /> : null}
          </Box>
          {sub.most > 1 ? (
            <Box>
              {Array.from({ length: sub.most }, (_, i) => (
                <SkeletonLine key={i} variant="body2" width={i === sub.most - 1 ? "60%" : "100%"} sx={i ? { display: sub.display(i) } : undefined} />
              ))}
            </Box>
          ) : (
            <SkeletonLine variant="body2" width={subtitleWidth} />
          )}
        </Stack>
      </Box>
      <Box sx={{ gap: 1.5, flexGrow: 1, display: "flex", alignItems: "center", justifyContent: wrapActions ? { xs: "flex-start", md: "flex-end" } : "flex-end", flexWrap: wrapActions ? "wrap" : "nowrap" }}>
        {acts.map((a, i) => (
          <Skeleton key={i} variant="rounded" width={a.width} sx={{ height: a.label ? 24 : tapHeight(36), flexShrink: 0 }} />
        ))}
      </Box>
    </Box>
  );
}

/** Loading twin of a controls card: a tab strip and / or a toolbar in one Card, no table (audit, DLQ). */
export function ControlsCardSkeleton({ tabs, toolbar, header = false }: { tabs?: ReactNode; toolbar?: ReactNode; /** A CardHeader title over the controls (template `CardHeader sx={{ mb: 2.5 }}`). */ header?: boolean }) {
  return (
    <Card aria-hidden="true" data-skel="controls">
      {header ? (
        <Box sx={{ mb: 2.5 }}>
          <CardHeaderSkeleton />
        </Box>
      ) : null}
      {tabs ? <Box sx={{ px: 2.5 }}>{tabs}</Box> : null}
      {toolbar}
    </Card>
  );
}
