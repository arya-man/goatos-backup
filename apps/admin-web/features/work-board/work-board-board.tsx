"use client";

import { Calendar, ChevronDown, ChevronLeft, ChevronRight, Info, Search } from "lucide-react";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { varAlpha } from "minimal-shared/utils";
import { KanbanBoard, KanbanColumn, KanbanItemRoot } from "@/components/minimal/kanban";
import { Label } from "@/components/minimal/label";
import { TaskPeopleDropdown } from "@/components/people-dropdown";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useMemo, useState, useTransition } from "react";
import { copy, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow, WorkBoardSummary } from "@/lib/api/work-board-server";
import {
  dayLabel,
  findOption,
  lanes,
  laneCursorParams,
  laneParkResetParams,
  moduleClass,
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
import { AvatarGroup } from "@/components/app/avatar";
import { ClockLabel, WorkProgress } from "./work-board-parts";
import { usePopover } from "minimal-shared/hooks";
import Checkbox from "@mui/material/Checkbox";
import Divider from "@mui/material/Divider";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import type { Theme } from "@mui/material/styles";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { TAP_MIN } from "@/components/minimal/_shared/tap";

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
  return { write, pending };
}

// The mock's assignee picker -- a stack of avatars, a "+N" chip, and a dropdown with a user
// search -- now lives in `components/assignee-picker.tsx` so the Tasks desk can host the same
// control as a form field. This board uses its `multi` mode, which is the picker exactly as it
// shipped here: the backend read takes ONE owner, so picking a person narrows to them, and the
// stack shows everyone on the page when nobody is picked.
// The mock's Module menu: a checkbox list of epic tags, "Clear all" / "Select all" at the foot,
// and the trigger reading "Module · all" or the first chosen tag "+N".
// `selected` empty means every module; `none` is the explicit empty selection after "Clear all".
function ModuleMenu({ pageContract, options, selected, none, onChange }: { pageContract: AdminUiPageContract; options: AdminUiOption[]; selected: string[]; none: boolean; onChange: (next: string[]) => void }) {
  // Template menu popover (CustomPopover + MenuList): MUI keeps it inside the viewport, closes it on
  // an outside click and on Escape, and hands focus back to the trigger.
  const menu = usePopover();
  const all = !none && (selected.length === 0 || selected.length === options.length);
  const chosen = none ? [] : all ? options.map((o) => o.key) : selected;
  const row = (theme: Theme) => ({ [theme.breakpoints.down("sm")]: { minHeight: TAP_MIN } });
  return (
    <div style={{ position: "relative", display: "inline-flex" }}>
      <button type="button" className="sel" aria-expanded={menu.open} aria-haspopup="menu" onClick={menu.onOpen}>
        {all ? (
          copy(pageContract, "filter.module.all")
        ) : chosen.length === 0 ? (
          copy(pageContract, "filter.module.none")
        ) : (
          <>
            <span className={moduleClass(chosen[0])}>{findOption(options, chosen[0])?.label ?? chosen[0]}</span>
            {chosen.length > 1 ? <span className="muted">+{chosen.length - 1}</span> : null}
          </>
        )}
        <ChevronDown className="ic" aria-hidden="true" />
      </button>
      <CustomPopover open={menu.open} anchorEl={menu.anchorEl} onClose={menu.onClose} slotProps={{ arrow: { placement: "top-left" } }}>
        {/* `.wb` scopes the board's epic-tag colours (`.wb .etag`, `.wb .e-*`) inside the portal. */}
        <MenuList className="wb" aria-label={copy(pageContract, "filter.module.all")}>
          {options.map((option) => {
            const on = chosen.includes(option.key);
            return (
              <MenuItem key={option.key} role="menuitemcheckbox" aria-checked={on} selected={on} sx={row} onClick={() => {
                const next = on ? chosen.filter((k) => k !== option.key) : [...chosen, option.key];
                onChange(next.length === options.length ? [] : next.length === 0 ? [PARAM_MODULE_NONE] : next);
              }}>
                <Checkbox size="small" checked={on} disableRipple tabIndex={-1} slotProps={{ input: { "aria-hidden": true } }} sx={{ p: 0 }} />
                <span className={moduleClass(option.key)}>{option.label}</span>
              </MenuItem>
            );
          })}
          <Divider sx={{ borderStyle: "dashed" }} />
          <MenuItem sx={(theme) => ({ ...row(theme), color: theme.palette.text.secondary, ...theme.typography.body2 })} onClick={() => onChange(all ? [PARAM_MODULE_NONE] : [])}>
            {all ? copy(pageContract, "filter.assignee.clear") : copy(pageContract, "filter.assignee.select_all")}
          </MenuItem>
        </MenuList>
      </CustomPopover>
    </div>
  );
}

// Board presentation (theme tokens only). Column width follows the template's
// `--kanban-column-width`, fitted so four lanes share a laptop row and a phone swipes one lane at a time.
const BOARD_SX = {
  "--kanban-column-width": { xs: "86vw", sm: "clamp(calc(var(--sp-5) * 6), calc((100% - 3 * var(--kanban-column-gap)) / 4), var(--kanban-col-w))" },
  overscrollBehaviorX: "contain",
  scrollSnapType: { xs: "x mandatory", md: "none" },
  "& > section": { scrollSnapAlign: "start" },
  // Columns grow with their cards (template KanbanColumn): no lane scroller -- the per-column
  // pager does the paging, so a phone never gets a scroll trap inside the board.
} as const;
const CARD_SX = {
  display: "flex",
  flexDirection: "column",
  gap: 1,
  px: 2,
  py: 2.5,
  minWidth: 0,
  overflow: "hidden",
  color: "inherit",
  textDecoration: "none",
  borderRadius: "inherit",
} as const;
const TITLE_SX = { display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden", overflowWrap: "anywhere" } as const;
const COUNTS_SX = {
  display: "flex",
  flexWrap: "wrap",
  columnGap: 1,
  rowGap: 0.5,
  minWidth: 0,
  typography: "caption",
  fontWeight: "fontWeightSemiBold",
  color: "text.secondary",
  "& b": { color: "text.primary" },
  "& > span": { minWidth: 0, overflowWrap: "anywhere" },
} as const;
const META_SX = {
  display: "grid",
  gridTemplateColumns: "minmax(0, 1fr) auto",
  alignItems: "center",
  columnGap: 1.25,
  rowGap: 1,
  minWidth: 0,
  "& .stack": { gridColumn: 2, gridRow: 2, justifySelf: "end", overflow: "hidden" },
} as const;
const KEY_SX = {
  gridColumn: "1 / -1",
  display: "inline-flex",
  alignItems: "center",
  gap: 0.75,
  minWidth: 0,
  typography: "caption",
  fontWeight: "fontWeightSemiBold",
  color: "text.secondary",
  overflowWrap: "anywhere",
} as const;
const EMPTY_SX = {
  listStyle: "none",
  p: 2,
  border: 1,
  borderStyle: "dashed",
  borderColor: "divider",
  borderRadius: "var(--r-lg)",
  typography: "body2",
  color: "text.disabled",
} as const;

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

function WorkCard({ pageContract, row, href }: { pageContract: AdminUiPageContract; row: WorkBoardRow; href: string }) {
  const moduleOpt = findOption(moduleOptions(pageContract), row.module);
  const hot = needsAttention(row);
  const total = row.counts.done + row.counts.pending;
  const split = pendingSplit(row);
  const stack = ownerStack(row);
  const ownerLabel = stack.names[0] || (row.owner_state === "pool" ? copy(pageContract, "owner.pool") : copy(pageContract, "owner.missing"));
  return (
    // Template kanban item: ItemRoot shell (paper, radius, z8 on hover) + ItemContent padding.
    <KanbanItemRoot sx={hot ? HOT_CARD_SX : CARD_ROOT_SX}>
    <Box component={LocalOverlayLink} href={href} scroll={false} aria-label={row.title} title={`${row.title} · ${ownerLabel}`} data-filter-row sx={CARD_SX}>
      <Typography component="div" variant="subtitle2" sx={TITLE_SX}>{row.title}</Typography>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5, minWidth: 0 }}>
        <span className={moduleClass(row.module)}>{moduleOpt?.label ?? row.module}</span>
        <span className="etag park" title={row.park_name || undefined}>{parkLabel(parkOptions(pageContract), row)}</span>
      </Box>
      {total > 0 ? <WorkProgress row={row} /> : null}
      <Box sx={COUNTS_SX}>
        {total > 0 ? (
          <span>
            <b>{row.counts.done}</b>/{total} {copy(pageContract, "card.done")}
          </span>
        ) : null}
        {split ? (
          <>
            {split.inReview > 0 ? <Box component="span" sx={{ color: "info.main" }}>{split.inReview} {copy(pageContract, "card.in_review")}</Box> : null}
            {split.started > 0 ? <span>{split.started} {copy(pageContract, "card.started")}</span> : null}
            {split.notStarted > 0 ? <span>{split.notStarted} {copy(pageContract, "card.not_started")}</span> : null}
          </>
        ) : (
          <>
            {row.lane === "in_review" && row.counts.pending > 0 ? <Box component="span" sx={{ color: "info.main" }}>{row.counts.pending} {copy(pageContract, "card.in_review")}</Box> : null}
            {row.lane === "in_progress" && row.counts.pending > 0 ? <span>{row.counts.pending} {copy(pageContract, "card.started")}</span> : null}
          </>
        )}
        {row.counts.needs_attention > 0 ? <Box component="span" sx={{ color: "warning.main" }}>{row.counts.needs_attention} {copy(pageContract, "card.attention")}</Box> : null}
      </Box>
      {/* Meta: the key line spans the card; the clock sits under it with the owner stack at its end. */}
      <Box sx={META_SX}>
        <Box component="span" title={row.subtitle || row.pen.operational_location_display || ""} sx={KEY_SX}>
          <span className={`ti${row.module === "counts" ? " p" : ""}`} aria-hidden="true">{row.module === "counts" ? "✓" : row.module === "vaccination" ? "◆" : "▣"}</span>
          <Box component="span" sx={{ minWidth: 0 }}>{row.subtitle || row.pen.operational_location_display || (moduleOpt?.label ?? row.module)}</Box>
        </Box>
        <Box sx={{ gridColumn: 1, minWidth: 0 }}><ClockLabel row={row} /></Box>
        <AvatarGroup className="stack" title={ownerLabel} names={stack.names} extra={stack.extra} size={22} empty={row.owner_state === "pool" ? "–" : "!"} emptyTone={row.owner_state === "missing" ? "danger" : undefined} />
      </Box>
    </Box>
    </KanbanItemRoot>
  );
}

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
}) {
  const { write, pending } = useUrlWriter();
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
      <div className="tbar" style={{ opacity: pending ? 0.7 : 1 }}>
        <label className="tsearch">
          <Search className="ic" aria-hidden="true" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={copy(pageContract, "filter.search")} aria-label={copy(pageContract, "filter.search")} />
        </label>
        {/* The owner filter is the Tasks page's people dropdown, verbatim (maintainer request,
            2026-09-19: follow the Tasks page): a labelled dropdown button over real checkboxes,
            the everyone row first. The board's URL carries ONE owner, so a second tick swaps the
            pick and the everyone row clears it. Every string comes from the page contract. */}
        {ownRowsOnly ? null : <TaskPeopleDropdown slot="assignee" label={copy(pageContract, "filter.assignee")} allLabel={copy(pageContract, "filter.assignee.all")} options={owners.map((o) => ({ id: o.id, name: o.name, title: `${cardsByOwner[o.id] ?? 0} ${copy(pageContract, "pager.rows")}` }))} selected={selectedOwner ? [selectedOwner] : []} onChange={(next) => write((p) => setParam(p, pageContract, PARAM_OWNER, next.find((id) => id !== selectedOwner)))} />}
        {parks.length > 1 ? (
          <nav className="parkpick" aria-label={copy(pageContract, "filter.park")}>
            <Link href={allParksHref} className={selectedPark === "" ? "on" : ""} aria-current={selectedPark === "" ? "true" : undefined}>
              {copy(pageContract, "filter.park.all")}
            </Link>
            {parks.map((park) => (
              <Link key={park.key} href={parkHrefs[park.key] ?? "#"} className={park.key === selectedPark ? "on" : ""} aria-current={park.key === selectedPark ? "true" : undefined}>
                {park.label}
              </Link>
            ))}
          </nav>
        ) : null}
        <div className="datenav">
          <Link href={previousDayHref} aria-label={copy(pageContract, "action.previous")}>‹</Link>
          <span className="d">
            <Calendar className="ic" aria-hidden="true" />
            <span>{dayLabel(businessDate)}</span>
            {isToday ? <Label variant="soft" color="primary">{copy(pageContract, "filter.date.today")}</Label> : null}
          </span>
          <Link href={nextDayHref} aria-label={copy(pageContract, "action.next")}>›</Link>
        </div>
        <ModuleMenu pageContract={pageContract} options={visibleModules} selected={selectedModules} none={noneSelected} onChange={(next) => write((p) => setParam(p, pageContract, PARAM_MODULE, next.length ? next.join(",") : undefined))} />
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
            <Info size={20} aria-hidden="true" />
          </IconButton>
        </Tooltip>
      </div>
      {/* Template sections/kanban: KanbanBoard track + KanbanColumn (count Label, h6 title) + item shells. */}
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
                  {column.key === "done" ? <Box component="span" sx={{ color: "success.main" }}>✓</Box> : null}
                </Box>
              }
            >
                {list.length ? (
                  list.map((row) => <WorkCard key={row.row_key} pageContract={pageContract} row={row} href={hrefForRow[row.row_key] ?? "#"} />)
                ) : (
                  // The header count is whole-filter; a column with work on OTHER pages but none
                  // on this one says so, instead of "Nothing here" under a non-zero count.
                  <Box component="li" sx={EMPTY_SX}>{count > 0 ? copy(pageContract, "lane.empty.other_pages") : copy(pageContract, "lane.empty")}</Box>
                )}
              {paged && !searching ? (
                // Each column pages on its own: the header stays the whole count, the footer
                // says which slice of it this is, as the last item of the lane.
                <Box component="li" sx={{ listStyle: "none" }}>
                <Box
                  component="nav"
                  className="colpager"
                  aria-label={`${column.label}: ${copy(pageContract, "section.board.aria")}`}
                  sx={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 1, minWidth: 0, pt: 1, typography: "caption", color: "text.secondary" }}
                >
                  <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5, flex: "none", whiteSpace: "nowrap", width: "100%" }}>
                    <Box component="span" sx={{ mr: "auto" }}>{first}–{last} {copy(pageContract, "drawer.subtasks.of")} {count}</Box>
                    {pager?.previousHref ? (
                      <Link className="iconbtn" href={pager.previousHref} aria-label={`${column.label}: ${copy(pageContract, "action.previous")}`}><ChevronLeft aria-hidden="true" /></Link>
                    ) : (
                      <span className="iconbtn" role="link" aria-disabled="true" aria-label={`${column.label}: ${copy(pageContract, "action.previous")}`}><ChevronLeft aria-hidden="true" /></span>
                    )}
                    {pager?.nextHref ? (
                      <Link className="iconbtn" href={pager.nextHref} aria-label={`${column.label}: ${copy(pageContract, "action.next")}`}><ChevronRight aria-hidden="true" /></Link>
                    ) : (
                      <span className="iconbtn" role="link" aria-disabled="true" aria-label={`${column.label}: ${copy(pageContract, "action.next")}`}><ChevronRight aria-hidden="true" /></span>
                    )}
                  </Box>
                </Box>
                </Box>
              ) : null}
            </KanbanColumn>
          );
        })}
      </KanbanBoard>
    </>
  );
}
