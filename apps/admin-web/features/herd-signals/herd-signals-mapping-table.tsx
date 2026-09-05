"use client";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useRouter } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import type { HerdSignalItem, HerdSignalsTagMappingResponse } from "@/lib/api/herd-signals";
import { MAPPING_LABEL, MAPPING_TONE, fmtBleMac, fmtAgo } from "./format";
import { useHerdSignalsNav } from "./herd-signals-nav-context";
import { herdSignalsHref, type HerdSignalsParams } from "./params";

// The Tag Mapping tab is NOT the live view with different filters: it answers "which BLE tag
// belongs to which animal identifier, who said so, and when", so it has its own ten columns
// (mock/herd-signals-mock.html, `renderMapping`). Rendering the live table here showed motion
// counts and battery on a screen about identity.
//
// Four of the mock's columns have NO field on the live contract today (HerdSignalItem in
// lib/api/herd-signals.ts): "Existing tag 1", "Existing tag 2" (the animal's non-BLE identifiers)
// and "Verified by" / "Verified at" (the mapping's provenance). They render "—". The names and
// dates in those columns of the reference design are fixture values, and reprinting one would
// state a verification that never happened. When the backend serves them, fill in the cell bodies
// here; the column set does not need to change.
const NOT_ON_CONTRACT = "—";

// The three verbs this screen owns, and only these three. There is deliberately no "mark as smart
// tag": every row here is ALREADY a BLE smart tag -- that is why the gateway reported it and why
// it is listed at all -- so asking an operator to declare one as such asserts nothing.
// `smart_tag_capable` is an internal consequence of binding, and the API exposes no endpoint for
// it (contracts/openapi/app-api.yaml).
type MappingAction = "map" | "replace" | "unmap";

const ACTION_TITLE: Record<MappingAction, string> = {
  map: "Map this tag to an animal",
  replace: "Replace this animal's tag",
  unmap: "Release this tag's binding",
};

// Rows-per-page choices must match the live table's, so switching tabs does not silently change
// the page size the reader had chosen.
const PAGE_SIZE_OPTIONS = [20, 50, 100];

type PagerWalk = { signature: string; stack: string[] };
const EMPTY_WALK: PagerWalk = { signature: "", stack: [] };

// Keyset pagination has no back-pointer on the wire, so "Previous" is only honest once we have
// actually WALKED forward and remember the cursor each page started from. The walk is module
// state (not React state) for the same reason the live table keeps its own: it must survive the
// server re-render that a page change triggers, and it must reset whenever the filter changes.
let pagerWalk: PagerWalk = EMPTY_WALK;
const pagerWalkListeners = new Set<() => void>();

function subscribePagerWalk(onChange: () => void): () => void {
  pagerWalkListeners.add(onChange);
  return () => {
    pagerWalkListeners.delete(onChange);
  };
}

function readPagerWalk(): PagerWalk {
  return pagerWalk;
}

function readServerPagerWalk(): PagerWalk {
  return EMPTY_WALK;
}

function writePagerWalk(next: PagerWalk): void {
  pagerWalk = next;
  for (const listener of pagerWalkListeners) listener();
}

// "unasked" rather than the obvious English word for a state that has not been asked for yet: the
// claim-boundary guard (tools/agent-hooks/check-herd-signals-language.mjs) bans that word outright
// on Herd Signals surfaces because it is one of the posture/behaviour paraphrases this module may
// never assert about an animal. A request-state name is not worth weakening that guard for.
type AnimalOption = {
  goat_id: string;
  display_id: string;
  animal_identifier_1: string | null;
  animal_identifier_2: string | null;
  sex: string;
  breed: string | null;
};

type LiveSmartTag = { tag_id: string; tag_mac: string };

async function postJson(path: string, body: unknown): Promise<{ ok: true; data: HerdSignalsTagMappingResponse } | { ok: false; error: string }> {
  try {
    const response = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      cache: "no-store",
      body: JSON.stringify(body),
    });
    const payload = (await response.json().catch(() => ({}))) as Partial<HerdSignalsTagMappingResponse> & { error?: string };
    // The backend's OWN message, verbatim: "that tag already belongs to another animal" and "this
    // animal already carries a live smart tag" are first-class answers this screen must show as
    // written, not flatten into a house error string.
    if (!response.ok) return { ok: false, error: payload.error ?? `The write failed (HTTP ${response.status}).` };
    return { ok: true, data: payload as HerdSignalsTagMappingResponse };
  } catch {
    return { ok: false, error: "The write could not reach the server. Nothing was changed." };
  }
}

function AnimalCell({ item }: { item: HerdSignalItem }) {
  if (!item.display_id) {
    return (
      <>
        <b className="muted">{NOT_ON_CONTRACT}</b>
        <small>No animal identifier</small>
      </>
    );
  }
  return (
    <>
      <b>{item.display_id}</b>
      <small>{MAPPING_LABEL[item.mapping_state].toLowerCase()}</small>
    </>
  );
}

// ---------------------------------------------------------------------------
// Animal picker
// ---------------------------------------------------------------------------

/**
 * Typeahead over the SAME bounded /goats/search read the Herd Register row list runs, through
 * /api/herd-signals/animals. Debounced at 300ms, identical to the filter bar's search — one
 * keystroke cadence across the module.
 *
 * The moment an animal is chosen it probes what that animal ALREADY carries
 * (/api/herd-signals/animals/smart-tags), so the conflict is on screen BEFORE the operator
 * submits rather than arriving as a 409 afterwards.
 */
function AnimalPicker({
  selected,
  onSelect,
  liveTags,
  liveTagsState,
}: {
  selected: AnimalOption | null;
  onSelect: (animal: AnimalOption | null) => void;
  liveTags: LiveSmartTag[];
  liveTagsState: "unasked" | "loading" | "ready" | "error";
}) {
  const [q, setQ] = useState("");
  const [results, setResults] = useState<AnimalOption[]>([]);
  const [state, setState] = useState<"unasked" | "loading" | "ready" | "error">("unasked");
  const [error, setError] = useState<string | null>(null);
  const debounceRef = useRef<number | null>(null);
  const requestRef = useRef(0);

  useEffect(() => {
    return () => {
      if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    };
  }, []);

  function runSearch(value: string) {
    setQ(value);
    onSelect(null);
    if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    const trimmed = value.trim();
    if (trimmed.length < 2) {
      setResults([]);
      setState("unasked");
      setError(null);
      return;
    }
    setState("loading");
    debounceRef.current = window.setTimeout(async () => {
      const ticket = ++requestRef.current;
      try {
        const response = await fetch(`/api/herd-signals/animals?q=${encodeURIComponent(trimmed)}`, {
          headers: { Accept: "application/json" },
          cache: "no-store",
        });
        const payload = (await response.json().catch(() => ({}))) as { items?: AnimalOption[]; error?: string };
        if (ticket !== requestRef.current) return;
        if (!response.ok) {
          setError(payload.error ?? `The animal search failed (HTTP ${response.status}).`);
          setResults([]);
          setState("error");
          return;
        }
        setError(null);
        setResults(payload.items ?? []);
        setState("ready");
      } catch {
        if (ticket !== requestRef.current) return;
        setError("The animal search could not reach the server.");
        setResults([]);
        setState("error");
      }
    }, 300);
  }

  return (
    <div className="hs-picker">
      <label htmlFor="hs-animal-q">Animal</label>
      <span className={`fsel search${q ? " has" : ""}`}>
        <svg className="ic sm" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="11" cy="11" r="7" />
          <path d="m20 20-3.5-3.5" />
        </svg>
        <input
          id="hs-animal-q"
          type="search"
          placeholder="Search by animal ID or ear-tag value"
          value={q}
          onChange={(event) => runSearch(event.target.value)}
          autoComplete="off"
        />
        {state === "loading" ? <span className="wfspin" aria-hidden="true" title="Searching" /> : null}
      </span>

      {selected ? (
        <div className="hs-picked">
          <div>
            <b>{selected.display_id}</b>
            <small>
              {selected.animal_identifier_1 ? `Tag ${selected.animal_identifier_1}` : "No ear-tag value on record"}
              {selected.breed ? ` · ${selected.breed}` : ""}
            </small>
          </div>
          <button type="button" className="btn sm" onClick={() => onSelect(null)}>
            Change
          </button>
        </div>
      ) : (
        <div className="hs-results" role="listbox" aria-label="Animal search results">
          {q.trim().length < 2 ? (
            <p className="faint small">Type at least two characters of an animal ID or ear-tag value.</p>
          ) : state === "error" ? (
            <p className="small" style={{ color: "var(--danger)" }}>{error}</p>
          ) : state === "loading" ? (
            <p className="faint small">Searching…</p>
          ) : results.length === 0 ? (
            <p className="faint small">No living animal matches that search.</p>
          ) : (
            results.map((animal) => (
              <button
                key={animal.goat_id}
                type="button"
                className="hs-result"
                role="option"
                aria-selected={false}
                onClick={() => onSelect(animal)}
              >
                <b>{animal.display_id}</b>
                <small>
                  {animal.animal_identifier_1 ? `Tag ${animal.animal_identifier_1}` : "No ear-tag value on record"}
                  {animal.breed ? ` · ${animal.breed}` : ""}
                </small>
              </button>
            ))
          )}
        </div>
      )}

      {/* What the chosen animal ALREADY carries. This is the whole point of probing before submit:
          an animal that already has a live smart tag needs REPLACE, not MAP. */}
      {selected ? (
        <p className="small faint" style={{ margin: "8px 0 0" }}>
          {liveTagsState === "loading"
            ? "Checking what this animal already carries…"
            : liveTagsState === "error"
              ? "Could not check what this animal already carries — the write will still be checked by the server."
              : liveTags.length === 0
                ? "This animal carries no live smart tag."
                : `This animal already carries ${liveTags.length === 1 ? "a live smart tag" : `${liveTags.length} live smart tags`}: ${liveTags.map((tag) => tag.tag_id).join(", ")}.`}
        </p>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Action dialog
// ---------------------------------------------------------------------------

function MappingDialog({
  action,
  item,
  onClose,
  onDone,
}: {
  action: MappingAction;
  item: HerdSignalItem;
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [animal, setAnimal] = useState<AnimalOption | null>(null);
  const [liveTags, setLiveTags] = useState<LiveSmartTag[]>([]);
  const [liveTagsState, setLiveTagsState] = useState<"unasked" | "loading" | "ready" | "error">("unasked");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const close = useCallback(() => {
    if (submitting) return;
    onClose();
  }, [onClose, submitting]);

  useEffect(() => {
    const node = panelRef.current;
    // The INPUT first, explicitly -- a plain "first focusable" query returns the header's Close
    // button (it comes earlier in the DOM), which swallowed every keystroke meant for the picker.
    const first =
      node?.querySelector<HTMLElement>('input:not([disabled])') ??
      node?.querySelector<HTMLElement>('button:not([disabled])');
    (first ?? node)?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    document.addEventListener("keydown", onKey);
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = previousOverflow;
    };
  }, [close]);

  // Reset the probe when the chosen animal changes, using React's "adjust state during render"
  // pattern (react.dev/learn/you-might-not-need-an-effect) rather than a setState inside the
  // Effect below -- clearing a stale answer is derived state, not synchronisation with the network.
  const [probedGoatId, setProbedGoatId] = useState<string | null>(null);
  if ((animal?.goat_id ?? null) !== probedGoatId) {
    setProbedGoatId(animal?.goat_id ?? null);
    setLiveTags([]);
    setLiveTagsState(animal ? "loading" : "unasked");
  }

  // Probe what the chosen animal already carries, so the conflict is visible before submit.
  useEffect(() => {
    if (!animal) return;
    let cancelled = false;
    (async () => {
      try {
        const response = await fetch(
          `/api/herd-signals/animals/smart-tags?goat_id=${encodeURIComponent(animal.goat_id)}&display_id=${encodeURIComponent(animal.display_id)}`,
          { headers: { Accept: "application/json" }, cache: "no-store" },
        );
        const payload = (await response.json().catch(() => ({}))) as { tags?: LiveSmartTag[] };
        if (cancelled) return;
        if (!response.ok) {
          setLiveTags([]);
          setLiveTagsState("error");
          return;
        }
        setLiveTags(payload.tags ?? []);
        setLiveTagsState("ready");
      } catch {
        if (cancelled) return;
        setLiveTags([]);
        setLiveTagsState("error");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [animal]);

  // A tag_mac equal to the tag id carries no extra value to claim; the contract wants the MAC only
  // when the tag reports one DISTINCT from its id.
  const distinctMac = item.tag_mac && item.tag_mac.toUpperCase() !== item.tag_id.toUpperCase() ? item.tag_mac : undefined;

  // Submit gating, with a SPECIFIC reason for every blocked state.
  let blockedReason: string | null = null;
  if (action === "map") {
    if (!animal) blockedReason = "Choose the animal this tag belongs to.";
    else if (liveTagsState === "loading") blockedReason = "Checking what this animal already carries…";
    else if (liveTags.length > 0) blockedReason = "This animal already carries a live smart tag — use Replace tag instead of Map.";
  } else if (action === "replace") {
    if (!animal) blockedReason = "Choose the animal whose tag this one replaces.";
    else if (liveTagsState === "loading") blockedReason = "Checking what this animal already carries…";
    else if (liveTagsState === "ready" && liveTags.length === 0)
      blockedReason = "This animal carries no live smart tag, so there is nothing to replace — use Map to animal.";
    else if (liveTags.some((tag) => tag.tag_id.toUpperCase() === item.tag_id.toUpperCase()))
      blockedReason = "This animal already carries exactly this tag.";
  }

  async function submit() {
    setSubmitting(true);
    setError(null);
    const result =
      action === "map"
        ? await postJson("/api/herd-signals/tag-mappings", {
            goat_id: animal?.goat_id,
            tag_id: item.tag_id,
            tag_mac: distinctMac,
          })
        : action === "replace"
          ? await postJson("/api/herd-signals/tag-mappings/replace", {
              goat_id: animal?.goat_id,
              new_tag_id: item.tag_id,
              new_tag_mac: distinctMac,
            })
          : await postJson("/api/herd-signals/tag-mappings/unmap", {
              tag_id: item.tag_id,
              tag_mac: distinctMac,
            });

    if (!result.ok) {
      // Keep the dialog open, keep the operator's choices, change nothing on the row: a refused
      // write left no half-state behind, and re-picking the animal from scratch is a punishment
      // for the server's answer.
      setSubmitting(false);
      setError(result.error);
      return;
    }

    const done =
      action === "map"
        ? `${item.tag_id} is now ${animal?.display_id}'s tag. Monitoring for this animal starts now.`
        : action === "replace"
          ? `${animal?.display_id} now carries ${item.tag_id}. A new monitoring period starts now.`
          : `${item.tag_id} is released. It is no longer any animal's tag.`;
    setSubmitting(false);
    onDone(done);
  }

  const needsAnimal = action === "map" || action === "replace";

  return (
    <>
      <div onClick={close} aria-hidden="true" style={{ position: "fixed", inset: 0, background: "var(--scrim)", zIndex: 210 }} />
      <div
        ref={panelRef}
        className="modal on card hs-mapping-modal"
        role="dialog"
        aria-modal="true"
        aria-label={ACTION_TITLE[action]}
        aria-busy={submitting}
        tabIndex={-1}
      >
        <div className="hd">
          <div>
            <h3>{ACTION_TITLE[action]}</h3>
            <div className="muted small" style={{ marginTop: 2 }}>
              BLE tag <span className="mono">{item.tag_id}</span>
              {item.tag_mac ? (
                <>
                  {" · MAC "}
                  <span className="mono">{fmtBleMac(item.tag_mac)}</span>
                </>
              ) : null}
            </div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn sm" onClick={close} disabled={submitting} aria-label="Close">
            Close
          </button>
        </div>

        <div className="bd">
          {needsAnimal ? (
            <AnimalPicker selected={animal} onSelect={setAnimal} liveTags={liveTags} liveTagsState={liveTagsState} />
          ) : (
            <p className="small" style={{ margin: 0 }}>
              {item.display_id
                ? `This releases the binding between ${item.tag_id} and ${item.display_id}.`
                : `This releases the binding for ${item.tag_id}.`}
            </p>
          )}

          {/* The monitoring boundary (backend/migrations/postgres 000196). Mapping stamps the
              instant this animal's monitoring begins; every packet the tag sent before it stays
              device telemetry and never enters this animal's baseline, pattern window, or
              correlations. The operator must not read a fresh mapping as inherited record. */}
          <div className="hs-boundary">
            {action === "unmap"
              ? "This tag keeps broadcasting and nothing already stored is deleted — it simply stops being attributed to an animal from now on, and no further animal-attributed value is produced for it."
              : "Monitoring for this animal starts at the moment you confirm. Everything this tag broadcast earlier stays device telemetry and never becomes part of this animal's record."}
          </div>

          {error ? (
            <div className="err on" role="alert">
              {error}
            </div>
          ) : null}
        </div>

        <div className="mf">
          {blockedReason && !error ? <span className="faint small" style={{ marginRight: "auto" }}>{blockedReason}</span> : null}
          <button type="button" className="btn sm" onClick={close} disabled={submitting}>
            Cancel
          </button>
          <button
            type="button"
            className="btn sm p"
            onClick={submit}
            disabled={submitting || blockedReason !== null}
            title={blockedReason ?? undefined}
            aria-busy={submitting}
          >
            {submitting ? <span className="wfspin" aria-hidden="true" /> : null}
            {submitting ? "Working…" : action === "map" ? "Map to animal" : action === "replace" ? "Replace tag" : "Unmap"}
          </button>
        </div>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Tab body: filter chips + actions + table + pagination
// ---------------------------------------------------------------------------

export function HerdSignalsMappingTable({
  items,
  nextCursor,
  params,
  tagsSeen,
}: {
  items: HerdSignalItem[];
  nextCursor: string | null;
  params: HerdSignalsParams;
  // The tenant-wide summary.tags_seen, NOT items.length — the pager readout and the empty state
  // must not assert a total they measured from one fetched page.
  tagsSeen: number;
}) {
  const router = useRouter();
  const { isPending, navigate } = useHerdSignalsNav();
  const [selectedTagId, setSelectedTagId] = useState<string | null>(null);
  const [dialog, setDialog] = useState<MappingAction | null>(null);
  const [flash, setFlash] = useState<string | null>(null);
  // The row snapshot a write asked the server to replace. `router.refresh()` resolves by delivering
  // NEW props, so "still refreshing" is exactly "the items we were handed are still the old ones" --
  // derived during render, not synchronised by an Effect that setStates on every prop change.
  const [refreshingFrom, setRefreshingFrom] = useState<HerdSignalItem[] | null>(null);
  if (refreshingFrom !== null && refreshingFrom !== items) setRefreshingFrom(null);
  const refreshing = refreshingFrom !== null;

  const filterSignature = herdSignalsHref(params, {});
  const walk = useSyncExternalStore(subscribePagerWalk, readPagerWalk, readServerPagerWalk);
  const stack = walk.signature === filterSignature ? walk.stack : [];

  const selected = items.find((item) => item.tag_id === selectedTagId) ?? null;
  // A selection that survived a filter/page change but is no longer on screen must not keep the
  // action buttons armed against a row the operator can no longer see.
  const selectionVisible = selected !== null;

  const onDone = useCallback(
    (message: string) => {
      setDialog(null);
      setFlash(message);
      setRefreshingFrom(items);
      // A server re-render of the same route: the table, the KPI strip and the tab counts all come
      // from the SAME /herd-signals/live response, so one refresh updates every one of them. Never
      // a location.reload() — the reader keeps their scroll, their filter and their selection.
      router.refresh();
    },
    [items, router],
  );

  const busy = isPending || refreshing;

  // ---- Disabled reasons. Every one of these is specific and true; "not available" tells the
  // operator nothing they can act on.
  function reasonFor(action: MappingAction): string | null {
    if (!selectionVisible) return "Select a tag to act on it";
    const item = selected as HerdSignalItem;
    if (item.mapping_state === "conflict") {
      return "This tag value resolves to more than one animal — fix the duplicate identifiers on the animal records first";
    }
    if (action === "map" && item.mapping_state === "mapped") {
      return `This tag is already mapped${item.display_id ? ` to ${item.display_id}` : ""} — use Replace tag`;
    }
    if (action === "replace" && item.mapping_state === "mapped") {
      return `This tag is already this animal's tag${item.display_id ? ` (${item.display_id})` : ""} — Replace puts a NEW tag on an animal`;
    }
    if (action === "unmap" && item.mapping_state !== "mapped") {
      return "This tag is not mapped to an animal, so there is no binding to release";
    }
    return null;
  }

  const actionButton = (action: MappingAction, label: string, primary: boolean) => {
    const reason = reasonFor(action);
    return (
      <button
        type="button"
        className={`btn sm${primary ? " p" : ""}`}
        disabled={reason !== null || busy}
        title={reason ?? ACTION_TITLE[action]}
        onClick={() => {
          setFlash(null);
          setDialog(action);
        }}
      >
        {label}
      </button>
    );
  };

  const nf = (value: number) => value.toLocaleString("en-IN");

  // ---- Pager, same shape as the live table's (mock/herd-signals-mock.html pagerHtml): Previous,
  // Next, a range/total/page readout that OMITS any clause whose source is unknown, then the
  // rows-per-page pill.
  const pageIndex = stack.length;
  const positionKnown = pageIndex === 0 || stack.length > 0;
  const rangeFrom = pageIndex * params.limit + 1;
  const rangeTo = pageIndex * params.limit + items.length;
  // Tag Mapping narrows only by shed/mapping-state/search, all of which the backend applies to
  // summary.tags_seen too -- so unlike the live tab there is no movement/pattern/KPI filter that
  // could make the tenant aggregate differ from this list's total.
  const total = tagsSeen > 0 ? tagsSeen : positionKnown && !nextCursor ? rangeTo : undefined;
  const pageCount = total ? Math.max(1, Math.ceil(total / params.limit)) : undefined;
  const prevHref = pageIndex > 0 ? herdSignalsHref(params, { hs_cursor: stack[pageIndex - 1] || undefined }) : null;
  const nextHref = nextCursor ? herdSignalsHref(params, { hs_cursor: nextCursor }) : null;

  function plainClick(event: React.MouseEvent): boolean {
    return !(event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey);
  }

  function goPrev(event: React.MouseEvent) {
    if (!prevHref || !plainClick(event)) return;
    event.preventDefault();
    writePagerWalk({ signature: filterSignature, stack: stack.slice(0, -1) });
    navigate(prevHref);
  }

  function goNext(event: React.MouseEvent) {
    if (!nextHref || !plainClick(event)) return;
    event.preventDefault();
    writePagerWalk({ signature: filterSignature, stack: [...stack, params.cursor ?? ""] });
    navigate(nextHref);
  }

  const pager = (variant: "top" | "bottom") => (
    <div className={`pager herd-signals-pager${variant === "top" ? " pager-top" : ""}`} aria-busy={busy}>
      {busy ? <span className="wfspin" aria-hidden="true" title="Loading" /> : null}
      {prevHref ? (
        <Link href={prevHref} className="pgbtn" onClick={goPrev}>
          &larr; Previous
        </Link>
      ) : (
        <button type="button" className="pgbtn" disabled>
          &larr; Previous
        </button>
      )}
      {nextHref ? (
        <Link href={nextHref} className="pgbtn" onClick={goNext}>
          Next &rarr;
        </Link>
      ) : (
        <button type="button" className="pgbtn" disabled>
          Next &rarr;
        </button>
      )}
      <span>
        Showing <b>{`${nf(rangeFrom)}–${nf(rangeTo)}`}</b>
        {total ? (
          <>
            {" of "}
            <b>{nf(total)}</b>
          </>
        ) : null}
        {" · page "}
        <b>{nf(pageIndex + 1)}</b>
        {pageCount ? (
          <>
            {" of "}
            <b>{nf(pageCount)}</b>
          </>
        ) : null}
      </span>
      <span className="sp" style={{ flex: 1 }} />
      <label className="fsel">
        <span>Rows</span>
        <select aria-label="Rows per page" value={params.limit} onChange={(event) => navigate(herdSignalsHref(params, { hs_limit: event.target.value }))}>
          {PAGE_SIZE_OPTIONS.map((size) => (
            <option key={size} value={size}>
              {size}
            </option>
          ))}
        </select>
      </label>
    </div>
  );

  const toolbar = (
    <div className="fbar" aria-busy={busy}>
      <a
        href={herdSignalsHref(params, { hs_map: undefined })}
        // "All" is the unfiltered default, not an explicit selection — the mock (which never
        // highlights any filter button, #tab-mapping's setMapFilter never toggles a class) renders
        // it as a plain neutral button. Only the two explicit filters (Unmapped/Conflict) get the
        // primary "p" highlight when chosen.
        className="btn sm"
        onClick={(event) => {
          if (!plainClick(event)) return;
          event.preventDefault();
          navigate(herdSignalsHref(params, { hs_map: undefined }));
        }}
      >
        All
      </a>
      <a
        href={herdSignalsHref(params, { hs_map: "unmapped" })}
        className={`btn sm${params.mappingState === "unmapped" ? " p" : ""}`}
        onClick={(event) => {
          if (!plainClick(event)) return;
          event.preventDefault();
          navigate(herdSignalsHref(params, { hs_map: "unmapped" }));
        }}
      >
        Unmapped only
      </a>
      <a
        href={herdSignalsHref(params, { hs_map: "conflict" })}
        className={`btn sm${params.mappingState === "conflict" ? " p" : ""}`}
        onClick={(event) => {
          if (!plainClick(event)) return;
          event.preventDefault();
          navigate(herdSignalsHref(params, { hs_map: "conflict" }));
        }}
      >
        Conflicts only
      </a>
      <div className="sp" style={{ flex: 1 }} />
      <span className="small faint">
        {selectionVisible ? (
          <>
            Selected <span className="mono">{selected?.tag_id}</span>
          </>
        ) : (
          "Select a tag to act on it"
        )}
      </span>
      {/* "Map to animal" is the PRIMARY verb here: on a farm where nothing is mapped yet it is the
          only action that makes this screen useful at all. */}
      {actionButton("map", "Map to animal", true)}
      {actionButton("replace", "Replace tag", false)}
      {actionButton("unmap", "Unmap", false)}
    </div>
  );

  const body = () => {
    if (items.length === 0) {
      return (
        <div className="empty">
          <div className="eicon">
            <svg className="ic" viewBox="0 0 24 24">
              <path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7" />
              <path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7L12.2 19" />
            </svg>
          </div>
          <h4>{params.mappingState ? "No tags in this mapping state" : "No BLE tags to map yet"}</h4>
          <p>
            {params.mappingState
              ? "Every tag in scope is in a different mapping state — an outcome, not a read failure."
              : "No BLE gateway has posted a tag for this tenant yet. Tags appear here the moment a gateway forwards one, mapped or not."}
          </p>
          {params.mappingState ? (
            <div className="eact">
              <Link href={herdSignalsHref(params, { hs_map: undefined })} className="btn sm">
                Show all tags
              </Link>
            </div>
          ) : null}
        </div>
      );
    }

    return (
      <>
        {pager("top")}
        <div className={`tblwrap${busy ? " wfbusy" : ""}`}>
          <table className="resp">
            <thead>
              <tr>
                <th>Animal</th>
                <th>Existing tag 1</th>
                <th>Existing tag 2</th>
                <th>Smart tag capable</th>
                <th>BLE tag ID</th>
                <th>BLE MAC</th>
                <th>Source</th>
                <th>Verified by</th>
                <th>Verified at</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => {
                const isSelected = item.tag_id === selectedTagId;
                return (
                  <tr
                    key={item.tag_id}
                    className={`hs-selectable${isSelected ? " on" : ""}`}
                    // Selecting is NOT navigation: the row arms the action bar and nothing else.
                    aria-selected={isSelected}
                    tabIndex={0}
                    onClick={() => {
                      setFlash(null);
                      setSelectedTagId(isSelected ? null : item.tag_id);
                    }}
                    onKeyDown={(event) => {
                      if (event.key !== "Enter" && event.key !== " ") return;
                      event.preventDefault();
                      setFlash(null);
                      setSelectedTagId(isSelected ? null : item.tag_id);
                    }}
                  >
                    <td data-l="Animal" className="wide animcell">
                      <AnimalCell item={item} />
                    </td>
                    <td data-l="Existing tag 1" className={item.animal_identifier_1 ? "" : "faint"}>
                      {item.animal_identifier_1 ? <span className="mono">{item.animal_identifier_1}</span> : NOT_ON_CONTRACT}
                    </td>
                    <td data-l="Existing tag 2" className={item.animal_identifier_2 ? "" : "faint"}>
                      {item.animal_identifier_2 ? <span className="mono">{item.animal_identifier_2}</span> : NOT_ON_CONTRACT}
                    </td>
                    {/* Every row on this screen IS a BLE smart tag -- that is why it is here at all.
                        smart_tag_capable is a flag on the ANIMAL IDENTIFIER, so an unmapped tag has
                        no identifier for the flag to live on and the honest value is "not
                        applicable", not "No". Rendering "No" asserted that a real smart tag is not a
                        smart tag. */}
                    <td data-l="Smart tag capable">
                      {item.mapping_state === "mapped" ? (
                        <Tag tone="ok">Yes</Tag>
                      ) : (
                        <span className="faint" title="No animal identifier yet, so there is no identifier to carry the flag">
                          {NOT_ON_CONTRACT}
                        </span>
                      )}
                    </td>
                    <td data-l="BLE tag ID">
                      <span className="mono">{item.tag_id}</span>
                    </td>
                    <td data-l="BLE MAC">
                      <span className="mono faint">{fmtBleMac(item.tag_mac)}</span>
                    </td>
                    {/* The only provenance the live contract carries: which gateway forwarded the
                        tag. A tag with no gateway id was not attributed to one, so it gets "—",
                        never a guessed source. */}
                    <td data-l="Source">{item.gateway_id ? "Gateway" : <span className="faint">{NOT_ON_CONTRACT}</span>}</td>
                    {/* TODO: Resolve mapped_by user ID to a human-readable operator name using the workforce
                        lookup pattern from the rest of the product (see docs for existing patterns).
                        For now, show the full ID; truncation to 8 chars can collide on UUIDs. */}
                    <td data-l="Bound by" className={item.mapped_by ? "" : "faint"}>
                      {item.mapped_by ? (
                        <span className="mono text-sm" title={`Operator ID: ${item.mapped_by}`}>
                          {item.mapped_by}
                        </span>
                      ) : (
                        NOT_ON_CONTRACT
                      )}
                    </td>
                    <td data-l="Bound at" className={item.mapped_at ? "" : "faint"}>
                      {item.mapped_at ? fmtAgo(item.mapped_at, new Date().getTime()) : NOT_ON_CONTRACT}
                    </td>
                    <td data-l="Status">
                      <Tag tone={MAPPING_TONE[item.mapping_state]}>{MAPPING_LABEL[item.mapping_state]}</Tag>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        {pager("bottom")}
      </>
    );
  };

  return (
    <div className="hs-mapping-tab">
      {toolbar}
      {flash ? (
        <div className="hs-flash" role="status">
          {flash}
        </div>
      ) : null}
      <div className="card">
        <div className="hd">
          <h3>BLE tag ↔ animal identifier mapping</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="small faint">Flag lives on the identifier, not the animal — an animal can carry several tags</span>
        </div>
        <div className="bd flush">{body()}</div>
      </div>
      {dialog && selected ? (
        <MappingDialog action={dialog} item={selected} onClose={() => setDialog(null)} onDone={onDone} />
      ) : null}
    </div>
  );
}
