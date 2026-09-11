"use client";

import { Calendar, ChevronDown, Search } from "lucide-react";
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

function setParam(params: URLSearchParams, key: string, value: string | undefined) {
  params.delete(PARAM_CURSOR);
  params.delete("page");
  params.delete(`${PARAM_CURSOR}_stack`);
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

// The mock's assignee picker: a stack of avatars, a "+N" chip, and a dropdown with a user search.
// The backend read takes ONE owner, so picking a person narrows to them; the stack shows everyone
// on the page when nobody is picked.
function AssigneePicker({ pageContract, owners, cardsByOwner, selected, onSelect }: { pageContract: AdminUiPageContract; owners: OwnerOption[]; cardsByOwner: Record<string, number>; selected?: string; onSelect: (id: string | undefined) => void }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const ref = useOutsideClose(open, () => {
    setOpen(false);
    setQuery("");
  });
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? owners.filter((o) => o.name.toLowerCase().includes(q)) : owners;
  }, [owners, query]);
  const current = owners.find((o) => o.id === selected);
  const visible = current ? [current] : owners.slice(0, 6);
  const overflow = current ? 0 : Math.max(0, owners.length - 6);
  const toggle = () => setOpen((v) => !v);
  return (
    <div ref={ref} className="avs" aria-label={copy(pageContract, "filter.assignee")}>
      {visible.map((o) => (
        <button type="button" key={o.id} className={`av${o.id === selected ? " on" : ""}`} title={o.name} aria-expanded={open} onClick={toggle}>
          {initials(o.name)}
        </button>
      ))}
      {overflow > 0 ? (
        <button type="button" className={`more${open ? " on" : ""}`} aria-expanded={open} aria-label={copy(pageContract, "filter.assignee")} onClick={toggle}>
          +{overflow}
        </button>
      ) : null}
      {owners.length === 0 || current ? (
        <button type="button" className={`more${open ? " on" : ""}`} aria-expanded={open} aria-label={copy(pageContract, "filter.assignee")} onClick={toggle} style={owners.length === 0 ? { width: "auto", padding: "0 10px", borderRadius: 999 } : undefined}>
          {owners.length === 0 ? copy(pageContract, "filter.assignee") : "▾"}
        </button>
      ) : null}
      {open ? (
        <div className="avmenu" role="listbox" aria-label={copy(pageContract, "filter.assignee")}>
          <div className="avq">
            <Search className="ic" aria-hidden="true" />
            <input ref={inputRef} value={query} onChange={(e) => setQuery(e.target.value)} placeholder={copy(pageContract, "filter.assignee.search")} aria-label={copy(pageContract, "filter.assignee.search")} autoComplete="off" />
          </div>
          <div className="list">
            {shown.map((o) => {
              const on = selected ? o.id === selected : true;
              return (
                <button type="button" key={o.id} role="option" aria-selected={o.id === selected} className={`opt${on ? " on" : ""}`} onClick={() => { onSelect(o.id === selected ? undefined : o.id); setOpen(false); setQuery(""); }}>
                  <span className="cb">{on ? "✓" : ""}</span>
                  <Avatar name={o.name} />
                  {o.name}
                  <span className="cnt">{cardsByOwner[o.id] ?? 0} {copy(pageContract, "pager.rows")}</span>
                </button>
              );
            })}
            {shown.length === 0 ? <div className="nomatch">{copy(pageContract, "filter.assignee.none")}</div> : null}
          </div>
          {selected ? (
            <button type="button" className="opt foot" onClick={() => { onSelect(undefined); setOpen(false); }}>
              {copy(pageContract, "filter.assignee.select_all")}
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

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
                <span className="cb">{on ? "✓" : ""}</span>
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

export function WorkBoardBoard({
  pageContract,
  rows,
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
  businessDate,
  isToday,
  previousDayHref,
  nextDayHref,
  hrefForRow,
}: {
  pageContract: AdminUiPageContract;
  rows: WorkBoardRow[];
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
  businessDate: string;
  isToday: boolean;
  previousDayHref: string;
  nextDayHref: string;
  hrefForRow: Record<string, string>;
}) {
  const { write, pending } = useUrlWriter();
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
  const searching = q.trim().length > 0;
  return (
    <>
      <div className="tbar" style={{ opacity: pending ? 0.7 : 1 }}>
        <label className="tsearch">
          <Search className="ic" aria-hidden="true" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={copy(pageContract, "filter.search")} aria-label={copy(pageContract, "filter.search")} />
        </label>
        {ownRowsOnly ? null : <AssigneePicker pageContract={pageContract} owners={owners} cardsByOwner={cardsByOwner} selected={selectedOwner} onSelect={(id) => write((p) => setParam(p, PARAM_OWNER, id))} />}
        {parks.length > 1 ? (
          <nav className="parkpick" aria-label={copy(pageContract, "filter.park")}>
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
        <ModuleMenu pageContract={pageContract} options={visibleModules} selected={selectedModules} none={noneSelected} onChange={(next) => write((p) => setParam(p, PARAM_MODULE, next.length ? next.join(",") : undefined))} />
      </div>
      <div className="rule">
        <span>{copy(pageContract, "board.rule")}</span>
        <span>{copy(pageContract, "board.attention")}</span>
      </div>
      <div className="board" role="group" aria-label={copy(pageContract, "section.board.aria")}>
        {columns.map((column) => {
          const list = byLane.get(column.key) ?? [];
          const count = searching || !summary ? list.length : summary.by_lane[column.key] ?? 0;
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
                  // The header count is whole-filter; a lane with work on OTHER pages but none on
                  // this one says so, instead of "Nothing here" under a non-zero count.
                  <div className="empty">{count > 0 ? copy(pageContract, "lane.empty.other_pages") : copy(pageContract, "lane.empty")}</div>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
}
