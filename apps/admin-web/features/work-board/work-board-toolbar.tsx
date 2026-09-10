"use client";

import { ChevronDown, Search } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState, useTransition } from "react";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { initials, PARAM_CURSOR, PARAM_MODULE, PARAM_OWNER, PARAM_STATE, toneOf, type OwnerOption } from "./work-board-model";

// The board's filter bar: module chips, status chips, and the assignee picker with a user search
// (the mock's dropdown). Every change writes the URL and lets the server re-read; nothing here
// filters rows on the client. Options and labels are the page contract's; the assignee list is
// the owners on the current page (see ownersOnPage).
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

function ChipToggle({
  options,
  selected,
  onToggle,
  allLabel,
  noneLabel,
  clearLabel,
  selectAllLabel,
}: {
  options: AdminUiOption[];
  selected: string[];
  onToggle: (next: string[]) => void;
  allLabel: string;
  noneLabel: string;
  clearLabel: string;
  selectAllLabel: string;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDoc = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("click", onDoc);
    return () => document.removeEventListener("click", onDoc);
  }, [open]);
  const all = selected.length === 0 || selected.length === options.length;
  const label = all ? allLabel : selected.length === 0 ? noneLabel : `${options.find((o) => o.key === selected[0])?.label ?? selected[0]}${selected.length > 1 ? ` +${selected.length - 1}` : ""}`;
  return (
    <span ref={ref} className={`sel${open ? " open" : ""}`} style={{ position: "relative" }}>
      <button type="button" className="btn sm" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {label}
        <ChevronDown className="ic" style={{ width: 13 }} aria-hidden="true" />
      </button>
      {open ? (
        <div className="menu" role="menu" style={{ display: "block", position: "absolute", top: "calc(100% + 6px)", left: 0, minWidth: 220, background: "var(--panel)", border: "1px solid var(--line)", borderRadius: 10, boxShadow: "var(--shadow)", padding: 6, zIndex: 30 }}>
          {options.map((option) => {
            const on = all || selected.includes(option.key);
            return (
              <button
                type="button"
                key={option.key}
                role="menuitemcheckbox"
                aria-checked={on}
                className="btn sm"
                style={{ display: "flex", width: "100%", justifyContent: "flex-start", gap: 8, border: 0, background: "transparent" }}
                onClick={() => {
                  const base = all ? options.map((o) => o.key) : selected;
                  const next = base.includes(option.key) ? base.filter((k) => k !== option.key) : [...base, option.key];
                  onToggle(next.length === options.length ? [] : next);
                }}
              >
                <input type="checkbox" readOnly checked={on} tabIndex={-1} aria-hidden="true" />
                <Tag tone={toneOf(option)}>{option.label}</Tag>
              </button>
            );
          })}
          <button type="button" className="btn sm" style={{ width: "100%", border: 0, background: "transparent", justifyContent: "flex-start", color: "var(--muted)", borderTop: "1px solid var(--line2)", marginTop: 4 }} onClick={() => onToggle(all ? ["__none__"] : [])}>
            {all ? clearLabel : selectAllLabel}
          </button>
        </div>
      ) : null}
    </span>
  );
}

function AssigneePicker({
  pageContract,
  owners,
  selected,
  ownRowsOnly,
  onSelect,
}: {
  pageContract: AdminUiPageContract;
  owners: OwnerOption[];
  selected: string | undefined;
  ownRowsOnly: boolean;
  onSelect: (id: string | undefined) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!open) return;
    inputRef.current?.focus();
    const onDoc = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) {
        setOpen(false);
        setQuery("");
      }
    };
    document.addEventListener("click", onDoc);
    return () => document.removeEventListener("click", onDoc);
  }, [open]);
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? owners.filter((o) => o.name.toLowerCase().includes(q)) : owners;
  }, [owners, query]);
  if (ownRowsOnly) return null;
  const current = owners.find((o) => o.id === selected);
  return (
    <div ref={ref} className="avs" style={{ position: "relative", display: "flex", alignItems: "center" }}>
      <button type="button" className="btn sm" aria-expanded={open} aria-label={copy(pageContract, "filter.assignee")} onClick={() => setOpen((v) => !v)}>
        {current ? (
          <>
            <span className="av xs">{initials(current.name)}</span> {current.name}
          </>
        ) : (
          <>
            {owners.slice(0, 5).map((o) => (
              <span key={o.id} className="av xs" title={o.name}>
                {initials(o.name)}
              </span>
            ))}
            {owners.length > 5 ? <span className="muted small">+{owners.length - 5}</span> : null}
            {owners.length === 0 ? copy(pageContract, "filter.assignee") : null}
          </>
        )}
        <ChevronDown className="ic" style={{ width: 13 }} aria-hidden="true" />
      </button>
      {open ? (
        <div role="listbox" aria-label={copy(pageContract, "filter.assignee")} style={{ position: "absolute", top: "calc(100% + 6px)", left: 0, width: 260, background: "var(--panel)", border: "1px solid var(--line)", borderRadius: 10, boxShadow: "var(--shadow)", padding: 6, zIndex: 30 }}>
          <div className="tsearch" style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", border: "1px solid var(--line)", borderRadius: 8, marginBottom: 4 }}>
            <Search className="ic" style={{ width: 14 }} aria-hidden="true" />
            <input ref={inputRef} value={query} onChange={(e) => setQuery(e.target.value)} placeholder={copy(pageContract, "filter.assignee.search")} aria-label={copy(pageContract, "filter.assignee.search")} style={{ border: 0, background: "transparent", outline: 0, flex: 1, font: "inherit", color: "inherit" }} />
          </div>
          <div style={{ maxHeight: 320, overflow: "auto" }}>
            {shown.map((o) => (
              <button
                type="button"
                key={o.id}
                role="option"
                aria-selected={o.id === selected}
                className="btn sm"
                style={{ display: "flex", width: "100%", justifyContent: "flex-start", gap: 9, border: 0, background: o.id === selected ? "var(--brand-soft)" : "transparent" }}
                onClick={() => {
                  onSelect(o.id === selected ? undefined : o.id);
                  setOpen(false);
                  setQuery("");
                }}
              >
                <span className="av xs">{initials(o.name)}</span>
                {o.name}
              </button>
            ))}
            {shown.length === 0 ? (
              <div className="muted small" style={{ padding: "6px 9px" }}>
                {copy(pageContract, "filter.assignee.none")}
              </div>
            ) : null}
          </div>
          {selected ? (
            <button type="button" className="btn sm" style={{ width: "100%", border: 0, background: "transparent", justifyContent: "flex-start", color: "var(--muted)", borderTop: "1px solid var(--line2)", marginTop: 4 }} onClick={() => { onSelect(undefined); setOpen(false); }}>
              {copy(pageContract, "filter.assignee.clear")}
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function WorkBoardToolbar({
  pageContract,
  moduleOptions,
  stateOptions,
  selectedModules,
  selectedStates,
  owners,
  selectedOwner,
  ownRowsOnly,
}: {
  pageContract: AdminUiPageContract;
  moduleOptions: AdminUiOption[];
  stateOptions: AdminUiOption[];
  selectedModules: string[];
  selectedStates: string[];
  owners: OwnerOption[];
  selectedOwner?: string;
  ownRowsOnly: boolean;
}) {
  const { write, pending } = useUrlWriter();
  return (
    <div className="fbar" style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap", opacity: pending ? 0.7 : 1 }}>
      <AssigneePicker pageContract={pageContract} owners={owners} selected={selectedOwner} ownRowsOnly={ownRowsOnly} onSelect={(id) => write((p) => setParam(p, PARAM_OWNER, id))} />
      <ChipToggle
        options={moduleOptions}
        selected={selectedModules}
        onToggle={(next) => write((p) => setParam(p, PARAM_MODULE, next.length ? next.join(",") : undefined))}
        allLabel={copy(pageContract, "filter.module.all")}
        noneLabel={copy(pageContract, "filter.module.none")}
        clearLabel={copy(pageContract, "filter.assignee.clear")}
        selectAllLabel={copy(pageContract, "filter.assignee.select_all")}
      />
      <ChipToggle
        options={stateOptions}
        selected={selectedStates}
        onToggle={(next) => write((p) => setParam(p, PARAM_STATE, next.length ? next.join(",") : undefined))}
        allLabel={copy(pageContract, "filter.state")}
        noneLabel={copy(pageContract, "filter.state")}
        clearLabel={copy(pageContract, "filter.assignee.clear")}
        selectAllLabel={copy(pageContract, "filter.assignee.select_all")}
      />
    </div>
  );
}
