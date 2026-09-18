"use client";

import { Calendar, Check, ChevronDown, Search } from "lucide-react";
import { TaskPeopleDropdown } from "@/components/people-dropdown";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState, useTransition } from "react";
import { copy, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow, WorkBoardSummary } from "@/lib/api/work-board-server";
import {
  barSegments,
  clockClass,
  dayLabel,
  findOption,
  initials,
  lanes,
  laneCursorParams,
  laneParkResetParams,
  moduleClass,
  moduleOptions,
  needsAttention,
  ownerStack,
  parkLabel,
  parkOptions,
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
  return { write, pending };
}

// A toolbar menu closes on an outside click and on Escape (Escape hands focus back to the
// trigger so a keyboard user is not dropped on the page body).
function useOutsideClose(open: boolean, close: () => void) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDoc = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) close();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      close();
      ref.current?.querySelector<HTMLElement>("[aria-expanded]")?.focus();
    };
    document.addEventListener("click", onDoc);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("click", onDoc);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open, close]);
  return ref;
}

function Avatar({ name, className = "" }: { name: string; className?: string }) {
  return (
    <span className={`av ${className}`.trim()} title={name}>
      {initials(name)}
    </span>
  );
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
  const [open, setOpen] = useState(false);
  const ref = useOutsideClose(open, () => setOpen(false));
  const all = !none && (selected.length === 0 || selected.length === options.length);
  const chosen = none ? [] : all ? options.map((o) => o.key) : selected;
  return (
    <div ref={ref} style={{ position: "relative", display: "inline-flex" }}>
      <button type="button" className="sel" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
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
      {open ? (
        <div className="menu" role="menu">
          {options.map((option) => {
            const on = chosen.includes(option.key);
            return (
              <button type="button" key={option.key} role="menuitemcheckbox" aria-checked={on} className={`opt${on ? " on" : ""}`} onClick={() => {
                const next = on ? chosen.filter((k) => k !== option.key) : [...chosen, option.key];
                onChange(next.length === options.length ? [] : next.length === 0 ? [PARAM_MODULE_NONE] : next);
              }}>
                <span className="cb" aria-hidden="true">{on ? <Check className="ic" strokeWidth={3} /> : null}</span>
                <span className={moduleClass(option.key)}>{option.label}</span>
              </button>
            );
          })}
          <button type="button" className="opt foot" onClick={() => onChange(all ? [PARAM_MODULE_NONE] : [])}>
            {all ? copy(pageContract, "filter.assignee.clear") : copy(pageContract, "filter.assignee.select_all")}
          </button>
        </div>
      ) : null}
    </div>
  );
}

function WorkCard({ pageContract, row, href }: { pageContract: AdminUiPageContract; row: WorkBoardRow; href: string }) {
  const moduleOpt = findOption(moduleOptions(pageContract), row.module);
  const hot = needsAttention(row);
  const total = row.counts.done + row.counts.pending;
  const seg = barSegments(row);
  const stack = ownerStack(row);
  const ownerLabel = stack.names[0] || (row.owner_state === "pool" ? copy(pageContract, "owner.pool") : copy(pageContract, "owner.missing"));
  return (
    <LocalOverlayLink href={href} scroll={false} className={`card${hot ? " hot" : ""}`} aria-label={row.title} title={`${row.title} · ${ownerLabel}`} data-filter-row>
      <div className="t">{row.title}</div>
      <span className={moduleClass(row.module)}>{moduleOpt?.label ?? row.module}</span>
      <span className="etag park" title={row.park_name || undefined}>{parkLabel(parkOptions(pageContract), row)}</span>
      {total > 0 ? (
        <div className="prog" aria-hidden="true">
          <i className="ok" style={{ width: `${seg.ok}%` }} />
          <i className="rev" style={{ width: `${seg.rev}%` }} />
          <i className="run" style={{ width: `${seg.run}%` }} />
          <i className="brk" style={{ width: `${seg.brk}%` }} />
        </div>
      ) : null}
      <div className="cnt">
        {total > 0 ? (
          <span>
            <b>{row.counts.done}</b>/{total} {copy(pageContract, "card.done")}
          </span>
        ) : null}
        {row.lane === "in_review" && row.counts.pending > 0 ? <span className="i">{row.counts.pending} {copy(pageContract, "card.in_review")}</span> : null}
        {row.lane === "in_progress" && row.counts.pending > 0 ? <span>{row.counts.pending} {copy(pageContract, "card.started")}</span> : null}
        {row.counts.needs_attention > 0 ? <span className="w">{row.counts.needs_attention} {copy(pageContract, "card.attention")}</span> : null}
      </div>
      <div className="row">
        <span className="key" title={row.subtitle || row.pen.operational_location_display || ""}>
          <span className={`ti${row.module === "counts" ? " p" : ""}`} aria-hidden="true">{row.module === "counts" ? "✓" : row.module === "vaccination" ? "◆" : "▣"}</span>
          {row.subtitle || row.pen.operational_location_display || (moduleOpt?.label ?? row.module)}
        </span>
        {row.clock_label ? <span className={`clk ${clockClass(row)}`.trim()}>{row.clock_label}</span> : null}
        <span className="sp" />
        <span className="stack" title={ownerLabel}>
          {stack.names.map((name) => <Avatar key={name} name={name} />)}
          {stack.extra > 0 ? <span className="av more">+{stack.extra}</span> : null}
          {stack.names.length === 0 ? <span className="av more" style={row.owner_state === "missing" ? { color: "var(--danger)" } : undefined}>{row.owner_state === "pool" ? "–" : "!"}</span> : null}
        </span>
      </div>
    </LocalOverlayLink>
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
            {isToday ? <span className="pill">{copy(pageContract, "filter.date.today")}</span> : null}
          </span>
          <Link href={nextDayHref} aria-label={copy(pageContract, "action.next")}>›</Link>
        </div>
        <ModuleMenu pageContract={pageContract} options={visibleModules} selected={selectedModules} none={noneSelected} onChange={(next) => write((p) => setParam(p, pageContract, PARAM_MODULE, next.length ? next.join(",") : undefined))} />
      </div>
      <div className="rule">
        <span>{copy(pageContract, "board.rule")}</span>
        <span>{copy(pageContract, "board.attention")}</span>
      </div>
      <div className="board" role="group" tabIndex={0} aria-label={copy(pageContract, "section.board.aria")}>
        {columns.map((column) => {
          const list = byLane.get(column.key) ?? [];
          const count = searching || !summary ? list.length : summary.by_lane[column.key] ?? 0;
          const pager = pagerFor.get(column.key);
          const page = pager?.rows.length ?? 0;
          const first = pager && page ? pager.offset + 1 : 0;
          const last = pager && page ? pager.offset + page : 0;
          const paged = Boolean(pager && (pager.nextHref || pager.previousHref));
          return (
            <div className="col" key={column.key} title={column.title}>
              <div className="ch">
                {column.label}
                {column.key === "done" ? <span className="chk">✓</span> : null}
                <span className="n">{count}</span>
              </div>
              <div className="cards">
                {list.length ? (
                  list.map((row) => <WorkCard key={row.row_key} pageContract={pageContract} row={row} href={hrefForRow[row.row_key] ?? "#"} />)
                ) : (
                  // The header count is whole-filter; a column with work on OTHER pages but none
                  // on this one says so, instead of "Nothing here" under a non-zero count.
                  <div className="empty">{count > 0 ? copy(pageContract, "lane.empty.other_pages") : copy(pageContract, "lane.empty")}</div>
                )}
              </div>
              {paged && !searching ? (
                // Each column pages on its own: the header stays the whole count, the footer
                // says which slice of it this is.
                <div className="colpager">
                  <span className="muted small">
                    <b>{first}–{last}</b> {copy(pageContract, "drawer.subtasks.of")} <b>{count}</b>
                  </span>
                  <span className="pgnav">
                    {pager?.previousHref ? (
                      <Link className="more" href={pager.previousHref} aria-label={`${column.label}: ${copy(pageContract, "action.previous")}`}>‹</Link>
                    ) : (
                      <span className="more" aria-disabled="true">‹</span>
                    )}
                    {pager?.nextHref ? (
                      <Link className="more" href={pager.nextHref} aria-label={`${column.label}: ${copy(pageContract, "action.next")}`}>›</Link>
                    ) : (
                      <span className="more" aria-disabled="true">›</span>
                    )}
                  </span>
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </>
  );
}
