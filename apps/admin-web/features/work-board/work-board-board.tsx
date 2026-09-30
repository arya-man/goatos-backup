"use client";

import { FOUR_LANE_COLUMN_WIDTH } from "@/components/app/kanban/board-layout";
import { WB_MODULE_SELECT_WIDTH, WB_SKELETON_LANES } from "./work-board-layout";
import Box from "@mui/material/Box";
import Select from "@mui/material/Select";
import Divider from "@mui/material/Divider";
import Tooltip from "@mui/material/Tooltip";
import Checkbox from "@mui/material/Checkbox";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import InputLabel from "@mui/material/InputLabel";
import Typography from "@mui/material/Typography";
import FormControl from "@mui/material/FormControl";
import InputAdornment from "@mui/material/InputAdornment";
import type { Theme } from "@mui/material/styles";
import { varAlpha } from "minimal-shared/utils";
import { KanbanBoard, KanbanColumn, KanbanItemRoot } from "@/components/app/kanban";
import { ItemContent, ItemInfo, ItemName, ItemStatus, type ItemStatusProps } from "@/components/app/kanban/item-styles";
import { Iconify } from "@/components/minimal/iconify";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { KanbanSkeleton } from "@/components/app/skeletons";
import { Label } from "@/components/minimal/label";
import { TaskPeopleDropdown } from "@/components/people-dropdown";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useId, useMemo, useState, useTransition } from "react";
import { copy, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow, WorkBoardSummary } from "@/lib/api/work-board-server";
import {
  clockClass,
  dayLabel,
  findOption,
  initials,
  lanes,
  laneCursorParams,
  laneParkResetParams,
  moduleOptions,
  needsAttention,
  ownerStack,
  parkLabel,
  parkOptions,
  pendingSplit,
  PARAM_CURSOR,
  PARAM_MODULE,
  PARAM_MODULE_NONE,
  PARAM_OWNER,
  type OwnerOption,
} from "./work-board-model";

// The board, in the mock's shape: search · assignee avatars · park pick · date nav · Module menu,
// the rule line, then four columns of cards. Park, date, module and assignee write the URL and the
// server re-reads; the search box is the one client-local filter (it narrows the cards on screen
// and the column count says how many are shown). Every label is the page contract's.

function setParam(params: URLSearchParams, pageContract: AdminUiPageContract, key: string, value: string | undefined) {
  params.delete(PARAM_CURSOR);
  params.delete("page");
  params.delete(`${PARAM_CURSOR}_stack`);
  const laneKeys = lanes(pageContract).map((lane) => lane.key);
  const parkKeys = parkOptions(pageContract).map((park) => park.key);
  for (const stale of Object.keys(laneCursorParams(laneKeys))) params.delete(stale);
  for (const stale of Object.keys(laneParkResetParams(laneKeys, parkKeys))) params.delete(stale);
  if (value) params.set(key, value);
  else params.delete(key);
}

function useUrlWriter() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [pending, startTransition] = useTransition();
  const write = (mutate: (params: URLSearchParams) => void) => {
    const params = new URLSearchParams(searchParams?.toString() ?? "");
    mutate(params);
    const qs = params.toString();
    startTransition(() => router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false }));
  };
  const navigate = (href: string) => startTransition(() => router.replace(href, { scroll: false }));
  return { write, navigate, pending };
}

// The Module filter: the template list toolbar's multi-select (sections/user/user-table-toolbar.tsx
// "Role": outlined FormControl + Select multiple, a Checkbox per row), plus the board's own
// "Clear all" / "Select all" row at the foot. `selected` empty means every module; `none` is the
// explicit empty selection after "Clear all".
const MODULE_TOGGLE = "__toggle";
const ALL_PARKS = "__all";
function ModuleSelect({ pageContract, options, selected, none, onChange }: { pageContract: AdminUiPageContract; options: AdminUiOption[]; selected: string[]; none: boolean; onChange: (next: string[]) => void }) {
  const inputId = useId();
  const all = !none && (selected.length === 0 || selected.length === options.length);
  const chosen = none ? [] : all ? options.map((o) => o.key) : selected;
  const label = copy(pageContract, "filter.module");
  // "Module · all" already names the field; the outlined label says "Module", so drop the prefix.
  // What is left starts a field value, so it reads like every other select ("All", not "all"; J2 P2-12).
  const bare = (text: string) => {
    const rest = text.replace(new RegExp(`^${label}\\s*·\\s*`), "");
    return rest.charAt(0).toUpperCase() + rest.slice(1);
  };
  const stated = all
    ? bare(copy(pageContract, "filter.module.all"))
    : chosen.length === 0
      ? bare(copy(pageContract, "filter.module.none"))
      : `${findOption(options, chosen[0])?.label ?? chosen[0]}${chosen.length > 1 ? ` +${chosen.length - 1}` : ""}`;
  return (
    <FormControl sx={{ flexShrink: 0, width: { xs: 1, md: WB_MODULE_SELECT_WIDTH } }}>
      <InputLabel htmlFor={inputId} shrink>{label}</InputLabel>
      <Select
        multiple
        displayEmpty
        label={label}
        value={chosen}
        renderValue={() => stated}
        inputProps={{ id: inputId }}
        MenuProps={{ slotProps: { paper: { sx: { maxHeight: 360 } } } }}
        onChange={(event) => {
          const raw = event.target.value;
          const next = typeof raw === "string" ? raw.split(",") : raw;
          if (next.includes(MODULE_TOGGLE)) {
            onChange(all ? [PARAM_MODULE_NONE] : []);
            return;
          }
          onChange(next.length === options.length ? [] : next.length === 0 ? [PARAM_MODULE_NONE] : next);
        }}
      >
        {options.map((option) => (
          <MenuItem key={option.key} value={option.key}>
            <Checkbox disableRipple size="small" checked={chosen.includes(option.key)} />
            {option.label}
          </MenuItem>
        ))}
        <Divider sx={{ borderStyle: "dashed" }} />
        <MenuItem value={MODULE_TOGGLE} sx={{ color: "text.secondary", typography: "body2" }}>
          {all ? copy(pageContract, "filter.assignee.clear") : copy(pageContract, "filter.assignee.select_all")}
        </MenuItem>
      </Select>
    </FormControl>
  );
}

// Raised card on the neutral column in both schemes: the template ItemRoot is grey[900] in dark (the
// page colour, darker than the column, no border, no resting shadow), so cards read as sunken.
// Paper + divider border + card shadow lifts them; a needs-attention card keeps its amber border
// over an opaque amber tint (invariants 3e582f46b, c0b294802).
const CARD_ROOT_SX = (t: Theme) => ({
  bgcolor: "background.paper",
  border: `1px solid ${t.vars.palette.divider}`,
  boxShadow: t.vars.customShadows.card,
  ...t.applyStyles("dark", { bgcolor: "background.paper" }),
});
const HOT_CARD_SX = (t: Theme) => ({
  ...CARD_ROOT_SX(t),
  borderColor: t.vars.palette.warning.main,
  backgroundImage: `linear-gradient(${varAlpha(t.vars.palette.warning.mainChannel, 0.08)}, ${varAlpha(t.vars.palette.warning.mainChannel, 0.08)})`,
});
const EMPTY_SX = {
  listStyle: "none",
  px: 0.5,
  typography: "caption",
  color: "text.disabled",
} as const;

// The template item's priority arrow reads the card's severity: broken / late is high, watch is medium.
function cardStatus(row: WorkBoardRow): ItemStatusProps["status"] {
  const cls = clockClass(row);
  return cls === "brk" ? "high" : cls === "run" ? "medium" : null;
}

function WorkCard({ pageContract, row, href }: { pageContract: AdminUiPageContract; row: WorkBoardRow; href: string }) {
  const moduleOpt = findOption(moduleOptions(pageContract), row.module);
  const hot = needsAttention(row);
  const total = row.counts.done + row.counts.pending;
  const split = pendingSplit(row);
  const stack = ownerStack(row);
  const ownerLabel = stack.names[0] || (row.owner_state === "pool" ? copy(pageContract, "owner.pool") : copy(pageContract, "owner.missing"));
  const assignee = stack.names.length
    ? [
        ...stack.names.map((name) => ({ id: name, name, initial: initials(name) })),
        ...(stack.extra > 0 ? [{ id: "extra", name: `+${stack.extra}`, initial: `+${stack.extra}` }] : []),
      ]
    : [{ id: "none", name: ownerLabel, initial: row.owner_state === "pool" ? "–" : "!", color: row.owner_state === "missing" ? ("error" as const) : ("default" as const) }];
  const reading = (color: string, text: React.ReactNode, key: string) => (
    <Box key={key} component="span" sx={{ typography: "caption", fontWeight: "fontWeightSemiBold", color }}>
      {text}
    </Box>
  );
  const readings = [
    total > 0 ? reading("text.secondary", <><Box component="b" sx={{ color: "text.primary" }}>{row.counts.done}</Box>/{total} {copy(pageContract, "card.done")}</>, "done") : null,
    ...(split
      ? [
          split.inReview > 0 ? reading("info.main", `${split.inReview} ${copy(pageContract, "card.in_review")}`, "rev") : null,
          split.started > 0 ? reading("text.secondary", `${split.started} ${copy(pageContract, "card.started")}`, "run") : null,
          split.notStarted > 0 ? reading("text.secondary", `${split.notStarted} ${copy(pageContract, "card.not_started")}`, "new") : null,
        ]
      : [
          row.lane === "in_review" && row.counts.pending > 0 ? reading("info.main", `${row.counts.pending} ${copy(pageContract, "card.in_review")}`, "rev") : null,
          row.lane === "in_progress" && row.counts.pending > 0 ? reading("text.secondary", `${row.counts.pending} ${copy(pageContract, "card.started")}`, "run") : null,
        ]),
    row.counts.needs_attention > 0 ? reading("warning.main", `${row.counts.needs_attention} ${copy(pageContract, "card.attention")}`, "hot") : null,
  ].filter(Boolean);
  const where = row.subtitle || row.pen.operational_location_display || "";
  // Template kanban item (sections/kanban/item): priority arrow, one-line name, then the ItemInfo
  // row. Module, park and the clock are words on the one caption line (the clock toned by severity),
  // never a strip of chips (TR1-#24); the done / in-review readings sit in the info row.
  const context = [where, moduleOpt?.label ?? row.module, parkLabel(parkOptions(pageContract), row)].filter(Boolean).join(" · ");
  const clockTone = cardStatus(row) === "high" ? "error.main" : cardStatus(row) === "medium" ? "warning.main" : undefined;
  return (
    // Template kanban item: ItemRoot shell + ItemContent (priority arrow, name, info row).
    <KanbanItemRoot sx={hot ? HOT_CARD_SX : CARD_ROOT_SX}>
      <Box
        component={LocalOverlayLink}
        href={href}
        scroll={false}
        aria-label={row.title}
        title={`${row.title} · ${ownerLabel}`}
        data-filter-row
        sx={{ display: "block", minWidth: 0, color: "inherit", textDecoration: "none", borderRadius: "inherit" }}
      >
        <ItemContent>
          <ItemStatus status={cardStatus(row)} />
          {/* Title and caption WRAP (TR2-P2-13; guard: work-board-card-wraps): long Mesha task
              names cut to "Milk preparation · Coi…" had no way to be read on a phone. */}
          <ItemName name={row.title} title={row.title} noWrap={false} sx={{ pr: 2.5, overflowWrap: "anywhere" }} />
          {context || row.clock_label ? (
            <Typography component="span" variant="caption" title={[context, row.park_name, row.clock_label].filter(Boolean).join(" · ")} sx={{ display: "block", mt: 0.5, color: "text.secondary", overflowWrap: "anywhere" }}>
              {context}
              {context && row.clock_label ? " · " : null}
              {row.clock_label ? <Box component="span" sx={{ color: clockTone, fontVariantNumeric: "tabular-nums" }}>{row.clock_label}</Box> : null}
            </Typography>
          ) : null}
          <ItemInfo assignee={assignee} assigneeTitle={ownerLabel}>
            {readings}
          </ItemInfo>
        </ItemContent>
      </Box>
    </KanbanItemRoot>
  );
}

// Board presentation (theme tokens only). Column width follows the template's
// `--kanban-column-width`, fitted so four lanes share a laptop row and a phone swipes one lane at a time.
const BOARD_SX = {
  "--kanban-column-width": FOUR_LANE_COLUMN_WIDTH,
  // The board is the containing block of its lanes' absolutely placed screen-reader items (an empty
  // lane's sr-only row): without it that row escaped the board's scroller, widened the document to
  // 1110px at 390 and the phone webview shrank the whole page to fit (guard: work-board-sr-only-contained).
  position: "relative",
  overscrollBehaviorX: "contain",
  scrollSnapType: { xs: "x mandatory", md: "none" },
  "& > section": { scrollSnapAlign: "start" },
  // Columns grow with their cards (template KanbanColumn): no lane scroller -- the per-column
  // pager does the paging, so a phone never gets a scroll trap inside the board.
} as const;

// One column's own page: its rows, its Next/Prev, and where in its list it sits.
export type WorkBoardLaneColumn = {
  lane: string;
  rows: WorkBoardRow[];
  nextHref: string | null;
  previousHref: string | null;
  pageNumber: number;
  offset: number;
};

export function WorkBoardBoard({
  pageContract,
  columns: laneColumns,
  summary,
  moduleOptions: visibleModules,
  selectedModules,
  noneSelected,
  owners,
  selectedOwner,
  ownRowsOnly,
  parks,
  selectedPark,
  parkHrefs,
  allParksHref,
  businessDate,
  isToday,
  previousDayHref,
  nextDayHref,
  hrefForRow,
  empty,
}: {
  pageContract: AdminUiPageContract;
  columns: WorkBoardLaneColumn[];
  summary: WorkBoardSummary | null;
  moduleOptions: AdminUiOption[];
  selectedModules: string[];
  noneSelected: boolean;
  owners: OwnerOption[];
  selectedOwner?: string;
  ownRowsOnly: boolean;
  parks: AdminUiOption[];
  selectedPark: string;
  parkHrefs: Record<string, string>;
  allParksHref: string;
  businessDate: string;
  isToday: boolean;
  previousDayHref: string;
  nextDayHref: string;
  hrefForRow: Record<string, string>;
  /** The empty board (template EmptyContent): rendered in place of the columns. */
  empty?: React.ReactNode;
}) {
  const { write, navigate } = useUrlWriter();
  const searchParams = useSearchParams();
  const boardParams = Object.fromEntries(searchParams?.entries() ?? []);
  const rows = useMemo(() => laneColumns.flatMap((column) => column.rows), [laneColumns]);
  const [q, setQ] = useState("");
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return needle ? rows.filter((r) => `${r.title} ${r.subtitle ?? ""} ${r.pen.operational_location_display ?? ""}`.toLowerCase().includes(needle)) : rows;
  }, [rows, q]);
  const cardsByOwner = useMemo(() => {
    const out: Record<string, number> = {};
    for (const r of rows) if (r.owner?.user_id) out[r.owner.user_id] = (out[r.owner.user_id] ?? 0) + 1;
    return out;
  }, [rows]);
  const columns = lanes(pageContract);
  const byLane = new Map<string, WorkBoardRow[]>();
  for (const row of shown) byLane.set(row.lane, [...(byLane.get(row.lane) ?? []), row]);
  const pagerFor = new Map(laneColumns.map((column) => [column.lane, column]));
  const searching = q.trim().length > 0;
  return (
    <>
      {/* Toolbar: the template list toolbar (sections/user/user-table-toolbar.tsx): outlined
          multi-select filters, the keyword field with a search adornment, then the calendar
          toolbar's day stepper (sections/calendar/calendar-toolbar.tsx) and the legend tooltip. */}
      <Box
        sx={{
          mb: 3,
          gap: 2,
          display: "flex",
          flexWrap: { md: "wrap", lg: "nowrap" },
          flexDirection: { xs: "column", md: "row" },
          alignItems: { xs: "stretch", md: "center" },
        }}
      >
        {/* The owner filter is the Tasks page's people dropdown, verbatim (maintainer request,
            2026-09-19: follow the Tasks page): an outlined multi-select over real checkboxes,
            the everyone row first. The board's URL carries ONE owner, so a second tick swaps the
            pick and the everyone row clears it. Every string comes from the page contract. */}
        {ownRowsOnly ? null : <TaskPeopleDropdown slot="assignee" label={copy(pageContract, "filter.assignee")} allLabel={copy(pageContract, "filter.assignee.all")} options={owners.map((o) => ({ id: o.id, name: o.name, title: `${cardsByOwner[o.id] ?? 0} ${copy(pageContract, "pager.rows")}` }))} selected={selectedOwner ? [selectedOwner] : []} onChange={(next) => write((p) => setParam(p, pageContract, PARAM_OWNER, next.find((id) => id !== selectedOwner)))} />}
        {parks.length > 1 ? (
          <TextField
            select
            label={copy(pageContract, "filter.park")}
            value={selectedPark || ALL_PARKS}
            onChange={(event) => navigate(event.target.value === ALL_PARKS ? allParksHref : parkHrefs[event.target.value] ?? allParksHref)}
            sx={{ flexShrink: 0, width: { xs: 1, md: 200 } }}
          >
            <MenuItem value={ALL_PARKS}>{copy(pageContract, "filter.park.all")}</MenuItem>
            {parks.map((park) => (
              <MenuItem key={park.key} value={park.key}>
                {park.label}
              </MenuItem>
            ))}
          </TextField>
        ) : null}
        <ModuleSelect pageContract={pageContract} options={visibleModules} selected={selectedModules} none={noneSelected} onChange={(next) => write((p) => setParam(p, pageContract, PARAM_MODULE, next.length ? next.join(",") : undefined))} />
        <TextField
          fullWidth
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={copy(pageContract, "filter.search")}
          slotProps={{
            htmlInput: { "aria-label": copy(pageContract, "filter.search") },
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
                </InputAdornment>
              ),
            },
          }}
          sx={{ minWidth: { md: 200 } }}
        />
        <Box sx={{ gap: 0.5, display: "flex", alignItems: "center", flexShrink: 0, justifyContent: { xs: "space-between", md: "flex-start" } }} aria-label={copy(pageContract, "filter.date")} role="group">
          <IconButton component={Link} href={previousDayHref} aria-label={copy(pageContract, "action.previous")}>
            <Iconify icon="eva:arrow-ios-back-fill" />
          </IconButton>
          <Box sx={{ gap: 1, display: "flex", alignItems: "center" }}>
            <Typography variant="h6" component="span" noWrap>{dayLabel(businessDate)}</Typography>
            {isToday ? <Label variant="soft" color="primary">{copy(pageContract, "filter.date.today")}</Label> : null}
          </Box>
          <IconButton component={Link} href={nextDayHref} aria-label={copy(pageContract, "action.next")}>
            <Iconify icon="eva:arrow-ios-forward-fill" />
          </IconButton>
          {/* Board legend (cd3972443): how a card's column is chosen and what amber means -- an info
              Tooltip beside the toolbar, not prose under it (FJ1-P2-1). */}
          <Tooltip
            title={
              <>
                <Box component="span" sx={{ display: "block" }}>{copy(pageContract, "board.rule")}</Box>
                <Box component="span" sx={{ display: "block", mt: 1 }}>{copy(pageContract, "board.attention")}</Box>
              </>
            }
          >
            <IconButton aria-label={`${copy(pageContract, "board.rule")} ${copy(pageContract, "board.attention")}`} sx={{ color: "text.secondary" }}>
              <Iconify icon="eva:info-outline" />
            </IconButton>
          </Tooltip>
        </Box>
      </Box>
      {/* Template sections/kanban: KanbanBoard track + KanbanColumn (count Label, h6 title) + item shells. */}
      {/* The board (guard: url-keyed-panel): an owner / park / module / day / lane-page change swaps it
          to the kanban skeleton at once; the toolbar stays on screen. */}
      <UrlSuspense searchParams={boardParams} watch={[ALL_PARAMS]} ignore={BOARD_IGNORE} fallback={<KanbanSkeleton lanes={WB_SKELETON_LANES} laneWidth={FOUR_LANE_COLUMN_WIDTH} />}>
      {empty ? empty : (
      <KanbanBoard role="group" tabIndex={0} aria-label={copy(pageContract, "section.board.aria")} sx={BOARD_SX}>
        {columns.map((column) => {
          const list = byLane.get(column.key) ?? [];
          const count = searching || !summary ? list.length : summary.by_lane[column.key] ?? 0;
          const pager = pagerFor.get(column.key);
          const page = pager?.rows.length ?? 0;
          const first = pager && page ? pager.offset + 1 : 0;
          const last = pager && page ? pager.offset + page : 0;
          const paged = Boolean(pager && (pager.nextHref || pager.previousHref));
          return (
            <KanbanColumn
              key={column.key}
              count={count}
              title={
                <Box component="span" title={column.title} sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
                  {column.label}
                  {column.key === "done" ? <Iconify icon="eva:checkmark-fill" width={16} sx={{ color: "success.main" }} /> : null}
                </Box>
              }
            >
                {list.length ? (
                  list.map((row) => <WorkCard key={row.row_key} pageContract={pageContract} row={row} href={hrefForRow[row.row_key] ?? "#"} />)
                ) : (
                  // The header count is whole-filter; a column with work on OTHER pages but none
                  // on this one says so, instead of "Nothing here" under a non-zero count.
                  // An empty template column is just the empty list (no dashed "Nothing here" box, TR1-#24).
                  count > 0 ? <Box component="li" sx={EMPTY_SX}>{copy(pageContract, "lane.empty.other_pages")}</Box> : <Box component="li" className="sr-only">{copy(pageContract, "lane.empty")}</Box>
                )}
              {paged && !searching ? (
                // Each column pages on its own: the header stays the whole count, the footer
                // says which slice of it this is, as the last item of the lane.
                <Box component="li" sx={{ listStyle: "none" }}>
                <Box
                  component="nav"
                  aria-label={`${column.label}: ${copy(pageContract, "section.board.aria")}`}
                  sx={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 0.5, minWidth: 0, typography: "caption", color: "text.secondary" }}
                >
                  <Box component="span" sx={{ mr: "auto", whiteSpace: "nowrap" }}>{first}–{last} {copy(pageContract, "drawer.subtasks.of")} {count}</Box>
                  {/* Template TablePaginationCustom's arrows: IconButtons, a disabled one when there is no page. */}
                  {pager?.previousHref ? (
                    <IconButton component={Link} href={pager.previousHref} aria-label={`${column.label}: ${copy(pageContract, "action.previous")}`}><Iconify icon="eva:arrow-ios-back-fill" /></IconButton>
                  ) : (
                    <IconButton disabled aria-label={`${column.label}: ${copy(pageContract, "action.previous")}`}><Iconify icon="eva:arrow-ios-back-fill" /></IconButton>
                  )}
                  {pager?.nextHref ? (
                    <IconButton component={Link} href={pager.nextHref} aria-label={`${column.label}: ${copy(pageContract, "action.next")}`}><Iconify icon="eva:arrow-ios-forward-fill" /></IconButton>
                  ) : (
                    <IconButton disabled aria-label={`${column.label}: ${copy(pageContract, "action.next")}`}><Iconify icon="eva:arrow-ios-forward-fill" /></IconButton>
                  )}
                </Box>
                </Box>
              ) : null}
            </KanbanColumn>
          );
        })}
      </KanbanBoard>
      )}
      </UrlSuspense>
    </>
  );
}

/** Params that never change the board: the card drawer and the action feedback banner. */
const BOARD_IGNORE = ["row", "action_status", "action_key"] as const;
