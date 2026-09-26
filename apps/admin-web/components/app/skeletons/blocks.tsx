"use client";

// Route + panel loading placeholders, built from the SAME layout parts the pages render (template
// CustomBreadcrumbs anatomy, MUI Tabs, KpiGrid, Card + CardHeader, Table + TableHead, the template
// TablePaginationCustom toolbar height), with MUI Skeleton inside them — the template's
// table-skeleton / product-item-skeleton usage. A line of text is a `variant="text"` Skeleton inside a
// Box carrying the real typography, so it takes the exact line height of the text it stands in for.
// A route's loading.tsx composes these to match its page; nothing here draws a page-specific shape.

import type { ReactNode } from "react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Divider from "@mui/material/Divider";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
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

import { BreadcrumbsContainer, BreadcrumbsContent, BreadcrumbsHeading, BreadcrumbsRoot } from "@/components/minimal/custom-breadcrumbs/styles";
import { KpiGrid } from "@/components/minimal/widgets/kpi-grid";

type Typo = "h3" | "h4" | "h5" | "h6" | "subtitle1" | "subtitle2" | "body1" | "body2" | "caption";

/** One line of text: a text Skeleton at the real typography's line height. */
export function SkeletonLine({ variant = "body2", width = "60%", sx }: { variant?: Typo; width?: number | string; sx?: SxProps<Theme> }) {
  return (
    <Box sx={[{ typography: variant, width, maxWidth: 1 }, ...(Array.isArray(sx) ? sx : [sx])]}>
      <Skeleton variant="text" animation="wave" />
    </Box>
  );
}

const wobble = (i: number, base = 44, span = 36) => `${base + ((i * 37) % span)}%`;

/**
 * The page root while loading: the SAME root the page renders (`.screen.on` by default — the 24px
 * block rhythm of the page column), busy for assistive tech. `root` is the page root's own class list
 * when it is not `screen on` (weights-page, vplan, wb, pagegrid …); `gap` mirrors a root that sets its
 * own gap.
 */
export function PageSkeleton({ children, root = "screen on", className, gap }: { children: ReactNode; root?: string; className?: string; gap?: number }) {
  return (
    <Box className={[root, className].filter(Boolean).join(" ")} aria-busy="true" data-skel-root="" sx={gap != null ? { display: "grid", gap } : undefined}>
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
  actions = 0,
  actionWidths,
  tabs,
  toolbar,
}: {
  /** The page shows a crumb trail (PageHeader hides one that only repeats the title). */
  crumbs?: boolean;
  /** Number of header action buttons (right-aligned, 36px template Button). */
  actions?: number;
  actionWidths?: number[];
  tabs?: ReactNode;
  toolbar?: ReactNode;
}) {
  const widths = actionWidths ?? Array.from({ length: actions }, (_, i) => (i === actions - 1 ? 128 : 104));
  return (
    <Box component="header" aria-hidden="true" data-skel="header" sx={{ display: "flex", flexDirection: "column", gap: "var(--sp-3)" }}>
      <BreadcrumbsRoot>
        <BreadcrumbsContainer>
          <BreadcrumbsContent>
            <BreadcrumbsHeading as="div">
              <Skeleton variant="text" width={220} sx={{ maxWidth: "60vw" }} />
            </BreadcrumbsHeading>
            {crumbs ? <SkeletonLine variant="body2" width={180} /> : null}
          </BreadcrumbsContent>
          {widths.length ? (
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5, alignItems: "center" }}>
              {widths.map((w, i) => (
                <Skeleton key={i} variant="rounded" width={w} height={36} />
              ))}
            </Box>
          ) : null}
        </BreadcrumbsContainer>
      </BreadcrumbsRoot>
      {tabs ? <div>{tabs}</div> : null}
      {toolbar ? <div>{toolbar}</div> : null}
    </Box>
  );
}

/** Loading twin of `AnimatedTabs`: the template MUI Tabs strip, labels as text Skeletons. */
export function TabsSkeleton({ count, variant = "underline", counts = false, sx }: { count: number; variant?: "underline" | "pill"; counts?: boolean; sx?: SxProps<Theme> }) {
  return (
    <Tabs
      value={false}
      variant="scrollable"
      scrollButtons={false}
      aria-hidden="true"
      data-skel="tabs"
      indicatorColor={variant === "pill" ? ("custom" as never) : undefined}
      sx={[variant === "pill" ? { borderRadius: "var(--r-md)" } : {}, ...(Array.isArray(sx) ? sx : [sx])]}
    >
      {Array.from({ length: count }, (_, i) => (
        <Tab
          key={i}
          disabled
          tabIndex={-1}
          label={
            <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
              <Skeleton variant="text" width={56 + ((i * 23) % 40)} />
              {counts ? <Skeleton variant="rounded" width={24} height={24} /> : null}
            </Box>
          }
        />
      ))}
    </Tabs>
  );
}

/** One outlined field (select / date / search) at the template TextField height. */
export function FieldSkeleton({ width = 200, grow = false, small = false }: { width?: number | string; grow?: boolean; small?: boolean }) {
  return <Skeleton variant="rounded" height={small ? 40 : 54} sx={{ width: grow ? "auto" : width, flexGrow: grow ? 1 : 0, flexShrink: 1, flexBasis: grow ? 240 : "auto", maxWidth: 1, minWidth: 0 }} />;
}

export type FilterField = number | "search" | "chip";

/**
 * Loading twin of the filter toolbars (FilterBar / WorklistFilters): a Card with `p 2.5`, fields
 * wrapping with `gap 2`. `fields` is the page's own field list: a number is a fixed-width field
 * (select / date), "search" takes the remaining width, "chip" is a chip-sized pill.
 * `inCard` drops the Card for a toolbar that sits inside another card.
 */
export function FilterCardSkeleton({ fields, actions = 0, inCard = false, small = false }: { fields: FilterField[] | number; actions?: number; inCard?: boolean; small?: boolean }) {
  const list: FilterField[] = typeof fields === "number" ? Array.from({ length: fields }, () => 200) : fields;
  const body = (
    <Box sx={{ p: 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center" }}>
      {list.map((f, i) =>
        f === "search" ? (
          <FieldSkeleton key={i} grow small={small} />
        ) : f === "chip" ? (
          <ChipSkeleton key={i} />
        ) : (
          <FieldSkeleton key={i} width={{ xs: "100%", sm: f } as never} small={small} />
        ),
      )}
      {Array.from({ length: actions }, (_, i) => (
        <Skeleton key={`a${i}`} variant="rounded" width={88} height={36} />
      ))}
    </Box>
  );
  if (inCard) return <div data-skel="filters">{body}</div>;
  return (
    <Card aria-hidden="true" data-skel="filters" sx={{ overflow: "visible" }}>
      {body}
    </Card>
  );
}

export type KpiShape = {
  /** Mini chart right of the figure (EcommerceWidgetSummary). */
  spark?: boolean;
  /** 48px round icon badge right of the figure (BankingWidgetSummary). */
  icon?: boolean;
  /** Trending row under the figure. */
  trend?: boolean;
  /** Muted helper line under the figure. */
  hint?: boolean;
  /** Two readings in one card (BookingCheckInWidgets split). */
  parts?: boolean;
  /** "tint" / "gradient" KpiCard (AnalyticsWidgetSummary: icon on top, h4 figure). */
  hero?: boolean;
};

/** Loading twin of the plain `KpiCard`: subtitle2 label, h3 figure (h4 at xs), optional rows. */
export function KpiCardSkeleton({ spark, icon, trend, hint, parts, hero }: KpiShape) {
  if (hero) {
    return (
      <Card aria-hidden="true" sx={{ p: { xs: 2, sm: 3 }, height: 1, boxShadow: "none" }}>
        {icon ? <Skeleton variant="rounded" width={48} height={48} sx={{ mb: { xs: 1.5, sm: 3 } }} /> : null}
        <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "flex-end", gap: 1 }}>
          <Box sx={{ flexGrow: 1, minWidth: 112 }}>
            <SkeletonLine variant="subtitle2" width="56%" sx={{ mb: 1 }} />
            {parts ? (
              <Stack direction="row" sx={{ mt: 1.5, gap: 2 }}>
                {[0, 1].map((i) => (
                  <Box key={i} sx={{ flex: "1 1 0" }}>
                    <SkeletonLine variant="h5" width="60%" sx={{ mb: 0.5 }} />
                    <SkeletonLine variant="body2" width="70%" />
                  </Box>
                ))}
              </Stack>
            ) : (
              <SkeletonLine variant="h4" width="46%" />
            )}
            {trend ? <SkeletonLine variant="subtitle2" width="48%" sx={{ mt: 1 }} /> : null}
            {hint ? <SkeletonLine variant="body2" width="72%" sx={{ mt: 0.5 }} /> : null}
          </Box>
          {spark ? <Skeleton variant="rounded" width={84} height={56} /> : null}
        </Box>
      </Card>
    );
  }
  return (
    <Card aria-hidden="true" sx={{ p: { xs: 2, sm: 3 }, height: 1 }}>
      <Box sx={{ display: "flex", flexWrap: spark ? "wrap" : "nowrap", alignItems: "center", gap: 2 }}>
        <Box sx={{ flex: spark ? "1 1 140px" : "1 1 auto", minWidth: 0 }}>
          <SkeletonLine variant="subtitle2" width="56%" />
          {parts ? (
            <Stack direction="row" sx={{ mt: 1.5, gap: 2 }}>
              {[0, 1].map((i) => (
                <Box key={i} sx={{ flex: "1 1 0" }}>
                  <SkeletonLine variant="h5" width="60%" sx={{ mb: 0.5 }} />
                  <SkeletonLine variant="body2" width="70%" />
                </Box>
              ))}
            </Stack>
          ) : (
            <Box sx={{ my: 1.5, typography: { xs: "h4", sm: "h3" } }}>
              <Skeleton variant="text" width="42%" />
            </Box>
          )}
          {trend ? <SkeletonLine variant="subtitle2" width="48%" /> : null}
          {hint ? <SkeletonLine variant="body2" width="72%" sx={{ mt: 0.5 }} /> : null}
        </Box>
        {spark ? (
          <Skeleton variant="rounded" width={84} height={56} />
        ) : icon ? (
          <Skeleton variant="circular" width={48} height={48} sx={{ flexShrink: 0, alignSelf: "flex-start" }} />
        ) : null}
      </Box>
    </Card>
  );
}

/**
 * Loading twin of `KpiGrid` + `KpiCard`: the same Grid (count-driven sizes, spacing 3), so the row
 * breaks exactly where the real deck breaks. `shapes` sets per-card anatomy when cards differ.
 */
export function KpiRowSkeleton({ count, shapes, ...shape }: { count: number; shapes?: KpiShape[] } & KpiShape) {
  return (
    <div data-skel="kpis" aria-hidden="true">
      <KpiGrid>
        {Array.from({ length: count }, (_, i) => (
          <KpiCardSkeleton key={i} {...(shapes?.[i] ?? shape)} />
        ))}
      </KpiGrid>
    </div>
  );
}

/**
 * Loading twin of `StatStrip` in its Card (template InvoiceAnalytic row): 56px ring, subtitle1 label,
 * optional body2 meta, subtitle2 figure; one row of dashed-divided cells up to four, rows of three or
 * four after that (the same `stripColumns` rule).
 */
export function StatStripSkeleton({ count, meta = false, card = true }: { count: number; meta?: boolean; card?: boolean }) {
  const cols = count <= 4 ? Math.max(count, 1) : count % 3 === 0 && count % 4 !== 0 ? 3 : 4;
  const cell = (i: number) => (
    <Box key={i} sx={{ width: 1, gap: 2.5, minWidth: 200, px: 2, display: "flex", alignItems: "center", justifyContent: "center" }}>
      <Skeleton variant="circular" width={56} height={56} sx={{ flexShrink: 0 }} />
      <Box sx={{ minWidth: 0, flex: "0 1 96px" }}>
        <SkeletonLine variant="subtitle1" width="100%" />
        {meta ? <SkeletonLine variant="body2" width="80%" sx={{ my: 0.5 }} /> : null}
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
      <Box sx={{ minHeight: "calc(var(--sp-6) * 2.25)", overflow: "hidden" }}>
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
export function CardHeaderSkeleton({ subheader = false, action = false }: { subheader?: boolean; action?: boolean | ReactNode }) {
  return (
    <CardHeader
      title={<Skeleton variant="text" width="32%" />}
      subheader={subheader ? <Skeleton variant="text" width="48%" /> : undefined}
      action={action === true ? <Skeleton variant="rounded" width={96} height={36} /> : action || undefined}
      sx={action && action !== true ? { alignItems: "center", flexWrap: "wrap", rowGap: 1.5, "& .MuiCardHeader-action": { m: 0, minWidth: 0, maxWidth: 1 } } : undefined}
    />
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
function SkeletonTableBody({ columns, rows, dense }: { columns: number; rows: number; dense?: boolean }) {
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
                  <Skeleton variant="text" width={c === 0 ? "76%" : wobble(r * 3 + c)} />
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
    <Box aria-hidden="true" sx={{ minHeight: "calc(var(--sp-4) * 2)", px: 2, display: "flex", alignItems: "center", justifyContent: "flex-end", columnGap: "var(--sp-3)" }}>
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
  tabs,
  toolbar,
  pager = true,
  dense = false,
  bare = false,
  id,
  children,
}: {
  /** The section's anchor id, so a hash link lands on the placeholder while the section streams. */
  id?: string;
  columns: number;
  rows?: number;
  header?: boolean;
  subheader?: boolean;
  headerAction?: boolean | ReactNode;
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
      {header ? <CardHeaderSkeleton subheader={subheader} action={headerAction} /> : null}
      {tabs ? <Box sx={{ px: 2.5, pt: header ? 2 : 0 }}>{tabs}</Box> : null}
      {toolbar}
      {children}
      <Box sx={{ pt: header && !toolbar && !tabs && !children ? 3 : 0 }}>
        <SkeletonTableBody columns={columns} rows={rows} dense={dense} />
      </Box>
      {pager ? <PagerSkeleton /> : null}
    </>
  );
  if (bare) return <div id={id} data-skel="table">{body}</div>;
  return (
    <Card id={id} aria-hidden="true" data-skel="table" sx={{ overflow: "hidden" }}>
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
}: {
  count: number;
  columns?: { xs?: number; sm?: number; md?: number; lg?: number; xl?: number };
  facts?: number;
  label?: boolean;
}) {
  const template = Object.fromEntries(Object.entries(columns).map(([bp, n]) => [bp, `repeat(${n}, 1fr)`]));
  return (
    <Box aria-hidden="true" data-skel="cards" sx={{ gap: "var(--sp-3)", display: "grid", gridTemplateColumns: template }}>
      {Array.from({ length: count }, (_, i) => (
        <Card key={i}>
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
export function DetailCardSkeleton({ rows = 5, columns = 1, header = true, height }: { rows?: number; columns?: number; header?: boolean; height?: number }) {
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
export function ToolbarSkeleton({ left, fields = [], small = true, sx }: { left?: ReactNode; fields?: FilterField[]; small?: boolean; sx?: SxProps<Theme> }) {
  return (
    <Box aria-hidden="true" data-skel="toolbar" sx={[{ display: "flex", flexWrap: "wrap", alignItems: "center", justifyContent: "space-between", gap: 2, minWidth: 0 }, ...(Array.isArray(sx) ? sx : [sx])]}>
      {left ? <Box sx={{ minWidth: 0, maxWidth: 1 }}>{left}</Box> : null}
      {fields.length ? (
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 2, alignItems: "center", minWidth: 0 }}>
          {fields.map((f, i) =>
            f === "search" ? <FieldSkeleton key={i} grow small={small} /> : f === "chip" ? <ChipSkeleton key={i} /> : <FieldSkeleton key={i} width={f} small={small} />,
          )}
        </Box>
      ) : null}
    </Box>
  );
}

/** One chip-sized pill (FilterChip / Label). */
export function ChipSkeleton({ width = 88 }: { width?: number }) {
  return <Skeleton variant="rounded" width={width} height={32} sx={{ borderRadius: "var(--r-md)", flexShrink: 0 }} />;
}

/** A row of chips (FilterChip rows, severity chips). */
export function ChipRowSkeleton({ count, widths }: { count: number; widths?: number[] }) {
  return (
    <Box aria-hidden="true" data-skel="chips" sx={{ display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center" }}>
      {Array.from({ length: count }, (_, i) => (
        <ChipSkeleton key={i} width={widths?.[i] ?? 72 + ((i * 29) % 40)} />
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
export function GridSkeleton({ items, spacing = 3 }: { items: { size: number | Record<string, number>; node: ReactNode }[]; spacing?: number }) {
  return (
    <div aria-hidden="true" data-skel="grid">
      <Grid container spacing={spacing}>
        {items.map((item, i) => (
          <Grid key={i} size={item.size as never} sx={{ minWidth: 0 }}>
            {item.node}
          </Grid>
        ))}
      </Grid>
    </div>
  );
}

/**
 * Loading twin of a lane board. `layout="kanban"` is the template KanbanBoard (336px lanes, 24px gap,
 * neutral lane paper, 16px item gap — the work board); `layout="grid"` is an equal-column board that
 * fills the width (action center / tasks boards), collapsing to 2 then 1 column like theirs.
 */
export function KanbanSkeleton({ lanes, layout = "kanban", minHeight }: { lanes: number[]; layout?: "kanban" | "grid"; minHeight?: number }) {
  const lane = (cards: number, i: number) => (
    <Box
      key={i}
      component="section"
      sx={{
        flexShrink: 0,
        display: "flex",
        flexDirection: "column",
        gap: 2,
        borderRadius: "var(--r-xl)",
        bgcolor: "background.neutral",
        minWidth: 0,
        ...(layout === "kanban" ? { width: "min(calc(var(--sp-6) * 7), calc(100vw - var(--sp-6)))" } : {}),
        ...(minHeight ? { minHeight } : {}),
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, pt: 2.5, px: 2 }}>
        <Skeleton variant="circular" width={24} height={24} />
        <SkeletonLine variant="h6" width="46%" />
      </Box>
      <Stack spacing={2} sx={{ px: 2, pb: 2, minHeight: "calc(var(--sp-5) * 2)" }}>
        {Array.from({ length: cards }, (_, c) => (
          <Card key={c} sx={{ p: 2.5, borderRadius: "var(--r-lg)", boxShadow: "none" }}>
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
          ? { pb: 2, columnGap: "var(--sp-3)", display: "flex", alignItems: "flex-start", overflow: "hidden", maxWidth: 1 }
          : { display: "grid", gap: 1.5, alignItems: "start", gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))", lg: `repeat(${lanes.length}, minmax(0, 1fr))` } }
      }
    >
      {lanes.map((cards, i) => lane(cards, i))}
    </Box>
  );
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

/** Loading twin of a controls card: a tab strip and / or a toolbar in one Card, no table (audit, DLQ). */
export function ControlsCardSkeleton({ tabs, toolbar }: { tabs?: ReactNode; toolbar?: ReactNode }) {
  return (
    <Card aria-hidden="true" data-skel="controls">
      {tabs ? <Box sx={{ px: 2.5 }}>{tabs}</Box> : null}
      {toolbar}
    </Card>
  );
}
