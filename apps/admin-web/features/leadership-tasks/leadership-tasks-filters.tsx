"use client";

import Link from "@/components/no-prefetch-link";
import { CalendarRange, ChevronDown, ListFilter, Search, SlidersHorizontal, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, useTransition } from "react";
import { useRouter, useSearchParams } from "next/navigation";

import { AssigneePicker } from "@/components/assignee-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { worklistFilterShownValue } from "@/lib/worklist-filter-value";
import { TASK_PAGING_PARAMS, TASK_PARAM } from "./params";
import { type TaskPeopleOption } from "./task-people-filter";
import { TASK_SORTS, type TaskSort } from "./task-url";
import { useDialogShell } from "./use-dialog-shell";

export type TaskPersonOption = TaskPeopleOption;
export type TaskStatusChip = {
  key: string;
  label: string;
  /**
   * The WHOLE-LIST count for this status under every other filter the reader has applied
   * (CONTRACT.md "Response envelope"). Never a page sum — a chip that counts the visible page
   * advertises rows the list is hiding.
   */
  count: number;
  selected: boolean;
};

/**
 * The Jira-shaped toolbar: free text, the two people pickers, both date spans, the status chips
 * and the sort control.
 *
 * WHY THIS IS HAND-ROLLED rather than `components/worklist-filters.tsx`, which this page would
 * otherwise reuse:
 *   1. That component has NO text-search kind (`features/health/health-config.tsx` renders the
 *      sanctioned hand-rolled search form BESIDE it for exactly this reason), and search here must
 *      be debounced rather than submitted.
 *   2. Its `daterange` kind is the only kind that writes both ends of a span at once — and it
 *      writes them through the app calendar, which needs a `DateRangePickerLabels` set the
 *      leadership-tasks page contract does not declare. Its `date` kind writes ONE end, and this
 *      endpoint answers 400 `invalid_date_range` for half a span, so a per-control push would put
 *      the reader on an error page for every first date they pick.
 *   3. It renders a `.tbar`, which cannot become a sheet. This page is opened inside the WhatsApp
 *      in-app webview at phone width, where a squeezed desktop bar is unusable.
 * The shared `.lt-fbar` / `.lt-fsel` / `.lt-chips` / `.lt-fnote` rules are reused verbatim, and
 * every string still comes from the page contract.
 *
 * On a phone the SAME DOM becomes a bottom sheet (`.lt-fgroup` → fixed, hidden until `.open`):
 * one control set, two layouts, so there is no second copy of the bar to drift.
 *
 * LAYOUT (rejected once as five stacked rows in a tall empty box, and rebuilt). The bar is ONE
 * dense row that wraps, the way a real issue tracker's is:
 *   search · status segment · assignee · raiser · sort · Deadline ▾ · Raised ▾ · active chips
 * Three things get it there. `.lt-fgroup` is `display:contents` above the sheet breakpoint, so its
 * children join the BAR's flex row instead of forming a second wrapping box inside it — that
 * nested box is what produced the rows and the dead vertical space. The status chips are one
 * segmented group rather than free-floating pills. And the four raw `dd/mm/yyyy` inputs, which
 * most sessions never touch, live behind two compact disclosures that show their span as a chip
 * once it is set; Apply moved INSIDE them, because a span is the only thing here that cannot
 * apply on change (half a span is a 400 on this endpoint) and it is the only thing that still
 * needs a button.
 */
export function LeadershipTasksFilters({
  pageContract,
  basePath,
  q,
  assignee,
  raiser,
  assigneeOptions,
  raiserOptions,
  assigneePinned,
  raiserPinned,
  deadlineFrom,
  deadlineTo,
  raisedFrom,
  raisedTo,
  rangeIncomplete,
  sort,
  statusChips,
  hasFilters,
  clearedHref,
  assigneeCounts = {},
  raiserCounts = {},
}: {
  pageContract: AdminUiPageContract;
  basePath: string;
  q: string;
  assignee: string;
  raiser: string;
  assigneeOptions: TaskPersonOption[];
  raiserOptions: TaskPersonOption[];
  /** The scope already pins the assignee (assigned_to_me), so the picker is inert. */
  assigneePinned: boolean;
  /** The scope already pins the raiser (assigned_by_me). */
  raiserPinned: boolean;
  deadlineFrom: string;
  deadlineTo: string;
  raisedFrom: string;
  raisedTo: string;
  /** A half-typed or inverted span, which is deliberately NOT being sent. */
  rangeIncomplete: boolean;
  sort: TaskSort;
  statusChips: TaskStatusChip[];
  hasFilters: boolean;
  clearedHref: string;
  /** How many cards on THIS page each person holds, shown beside their name in the picker's
   *  list exactly as the Work Board shows "N rows" (`board.on_this_page` is the unit here). */
  assigneeCounts?: Record<string, number>;
  raiserCounts?: Record<string, number>;
}) {
  const router = useRouter();
  const routerSearchParams = useSearchParams();
  const [isPending, startTransition] = useTransition();
  const [sheetOpen, setSheetOpen] = useState(false);
  /** Whether the ONE dates disclosure (both spans inside it) is open. */
  const [openRange, setOpenRange] = useState<"dates" | null>(null);
  const sheetRef = useRef<HTMLDivElement>(null);
  const rangesRef = useRef<HTMLDivElement>(null);
  const moreRef = useRef<HTMLButtonElement>(null);
  const current = routerSearchParams?.toString() ?? "";

  /**
   * WHAT THE CONTROLS SHOW WHILE THE SERVER IS STILL ANSWERING.
   *
   * The list query is ~230ms+ on the OCI database, so a select that keeps rendering the server's
   * last value until the round trip lands reads as "the filter is not applying" — the exact defect
   * `lib/worklist-filter-value.ts` was written for, and `components/worklist-filters.tsx` already
   * solves the same way. The optimistic search is stamped with the URL it was made FROM, so it is
   * dropped implicitly on the next render with a new URL: a navigation that fails or is superseded
   * falls back to the server's answer rather than leaving a control showing something that never
   * happened.
   */
  const [optimisticSearch, setOptimisticSearch] = useState<{ from: string; search: string } | null>(
    null,
  );
  const optimisticActive = optimisticSearch?.from === current;
  const effectiveSearch = optimisticActive ? optimisticSearch.search : current;
  const effectiveParams = useMemo(() => new URLSearchParams(effectiveSearch), [effectiveSearch]);
  /**
   * Every control on this bar is clearable (its empty option is "All"), so an ABSENT parameter
   * during a pending navigation means "just cleared" and must render empty rather than falling
   * through to the stale server prop.
   */
  const fieldValue = (param: string, serverValue: string) =>
    worklistFilterShownValue(effectiveParams.get(param), serverValue, Boolean(optimisticActive), true);

  // Synced from the URL on every navigation with React's adjust-state-during-render pattern
  // (react.dev/learn/you-might-not-need-an-effect), never an Effect that setStates synchronously:
  // the debounced local edits below must keep priority between renders.
  const [syncedQ, setSyncedQ] = useState(q);
  const [text, setText] = useState(q);
  if (q !== syncedQ) {
    setSyncedQ(q);
    setText(q);
  }
  // Both ends of each span are staged locally and committed together — never one end at a time.
  const [dates, setDates] = useState({ deadlineFrom, deadlineTo, raisedFrom, raisedTo });
  const [syncedDates, setSyncedDates] = useState({ deadlineFrom, deadlineTo, raisedFrom, raisedTo });
  const applied = `${deadlineFrom}|${deadlineTo}|${raisedFrom}|${raisedTo}`;
  if (`${syncedDates.deadlineFrom}|${syncedDates.deadlineTo}|${syncedDates.raisedFrom}|${syncedDates.raisedTo}` !== applied) {
    setSyncedDates({ deadlineFrom, deadlineTo, raisedFrom, raisedTo });
    setDates({ deadlineFrom, deadlineTo, raisedFrom, raisedTo });
  }

  const debounceRef = useRef<number | null>(null);
  /**
   * One writer for the whole bar: it records the optimistic URL, then navigates inside a
   * transition so `isPending` can ring the bar instead of the page blanking.
   */
  const go = useCallback(
    (next: URLSearchParams) => {
      const qs = next.toString();
      setOptimisticSearch({ from: current, search: qs });
      startTransition(() =>
        router.replace(qs ? `${basePath}?${qs}` : basePath, { scroll: false }),
      );
    },
    [basePath, current, router],
  );
  useEffect(
    () => () => {
      if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    },
    [],
  );

  /**
   * The current URL with some parameters replaced. Every filter change also DROPS the three
   * paging params and the one-shot feedback pair: a cursor minted over one result set means
   * nothing over another, and the backend rejects a cursor minted under a different sort outright.
   */
  const paramsWith = (overrides: Record<string, string>) => {
    const next = new URLSearchParams(effectiveSearch);
    for (const [param, value] of Object.entries(overrides)) {
      if (value) next.set(param, value);
      else next.delete(param);
    }
    // Paging is RESET by every filter change. The backend rejects a cursor minted under a
    // different sort outright (400 `invalid_cursor`) rather than serving a wrong page, and a
    // cursor from one filtered result set means nothing in another — so the cursor, the page
    // number and the whole cursor STACK go.
    for (const param of TASK_PAGING_PARAMS) next.delete(param);
    next.delete(TASK_PARAM.feedbackStatus);
    next.delete(TASK_PARAM.feedbackCode);
    return next;
  };
  const hrefWith = (overrides: Record<string, string>) => {
    const qs = paramsWith(overrides).toString();
    return qs ? `${basePath}?${qs}` : basePath;
  };

  function onSearchChange(value: string) {
    setText(value);
    if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    // 300ms, the same beat as features/herd-signals/herd-signals-filters.tsx. There is no shared
    // debounce hook in this workspace; that file is the precedent, not an oversight.
    debounceRef.current = window.setTimeout(() => {
      go(paramsWith({ [TASK_PARAM.q]: value.trim() }));
    }, 300);
  }

  /** The status chip highlighted RIGHT NOW, optimistically. */
  const selectedChipKey = fieldValue(
    TASK_PARAM.filter,
    statusChips.find((chip) => chip.selected)?.key ?? "all",
  );
  const chipSelected = (key: string) => (selectedChipKey || "all") === key;

  /**
   * The sheet is a real overlay on a phone, so it owes the reader the same three things the
   * modals do: Escape, a body scroll lock (a drag inside it was scrolling the LIST behind it) and
   * a focus trap. `.lt-fmore` — the only way to open it — is `display:none` above the sheet
   * breakpoint, so this state is unreachable on a desktop viewport and the lock cannot strand a
   * wide page. Focus returns to the opener on close.
   */
  const closeSheet = useCallback(() => {
    setSheetOpen(false);
    moreRef.current?.focus();
  }, []);
  useDialogShell({ open: sheetOpen, onClose: closeSheet, containerRef: sheetRef });
  useEffect(() => {
    if (!sheetOpen) return;
    const node = sheetRef.current;
    const first = node?.querySelector<HTMLElement>("a[href],button:not([disabled])");
    (first ?? node)?.focus();
  }, [sheetOpen]);

  useEffect(() => {
    if (!openRange) return;
    function onDown(event: MouseEvent) {
      const element = event.target as HTMLElement | null;
      if (element && rangesRef.current?.contains(element)) return;
      setOpenRange(null);
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") setOpenRange(null);
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [openRange]);

  const searchLabel = copy(pageContract, "filter.search_label");
  /**
   * The placeholder and the long-form hint are two keys. At 2000px the search box is ~160px of
   * input and "Search tasks, or type a number" is ~200px of text, so the placeholder clipped to
   * "Search tasks, or type a n…" -- the bar's first control was truncating its own label. The
   * placeholder is now the short form; the number affordance lives in the title / aria-label.
   */
  const searchHint = copy(pageContract, "filter.search_hint", "Search by title, or type a task number");
  /** What a date disclosure reads when no span is applied: "Deadline · any". */
  const anyLabel = copy(pageContract, "filter.range_any", "any");
  const rangeNote = copy(pageContract, "filter.range_note");
  const allOption = copy(pageContract, "filter.all_option");
  const applyLabel = copy(pageContract, "action.apply");
  const clearLabel = copy(pageContract, "action.clear");
  const removeLabel = copy(pageContract, "filter.remove_label", clearLabel);
  const assigneeLabel = copy(pageContract, "filter.assignee");
  const raiserLabel = copy(pageContract, "filter.raiser");
  const deadlineLabel = copy(pageContract, "filter.deadline", copy(pageContract, "label.deadline", "Deadline"));
  const raisedLabel = copy(pageContract, "filter.raised", "Raised");
  const peopleSearchLabel = copy(pageContract, "filter.people_search", "Type a name");
  const onThisPageLabel = copy(pageContract, "board.on_this_page", "on this page");
  const datesLabel = copy(pageContract, "filter.dates", "Dates");
  const peopleNoMatchesLabel = copy(pageContract, "filter.people_no_matches", "Nobody by that name.");

  /**
   * A span as the disclosure's own label: the reader should not have to open it to learn whether
   * it is narrowing the list. An EN DASH is glue between two ISO dates, not copy.
   */
  const spanLabel = (from: string, to: string) => (from && to ? `${from} – ${to}` : from || to);
  const deadlineSpan = spanLabel(deadlineFrom, deadlineTo);
  const raisedSpan = spanLabel(raisedFrom, raisedTo);

  const shownAssignee = fieldValue(TASK_PARAM.assignee, assignee);
  const shownRaiser = fieldValue(TASK_PARAM.raiser, raiser);
  const labelFor = (options: TaskPersonOption[], value: string) =>
    options.find((option) => option.value === value)?.label ?? value;

  /**
   * WHAT IS NARROWING THE LIST, as chips that each remove exactly themselves. Status lives in the
   * segmented control and sort is not a narrowing, so neither is repeated here; everything that
   * hides rows is. The idiom is `components/worklist-filters.tsx`'s -- the applied value stated
   * beside its own control, with a single affordance that takes it back off.
   */
  // DATA, not closures: an object literal built during render may not capture a ref read
  // (react-hooks "Cannot access refs during render"), so each chip carries the parameter
  // OVERRIDES that take it back off and the JSX handler below is what applies them.
  const activeChips: { key: string; label: string; overrides: Record<string, string> }[] = [];
  if (text.trim()) {
    activeChips.push({ key: TASK_PARAM.q, label: text.trim(), overrides: { [TASK_PARAM.q]: "" } });
  }
  if (shownAssignee && !assigneePinned) {
    activeChips.push({
      key: TASK_PARAM.assignee,
      label: `${assigneeLabel}: ${labelFor(assigneeOptions, shownAssignee)}`,
      overrides: { [TASK_PARAM.assignee]: "" },
    });
  }
  if (shownRaiser && !raiserPinned) {
    activeChips.push({
      key: TASK_PARAM.raiser,
      label: `${raiserLabel}: ${labelFor(raiserOptions, shownRaiser)}`,
      overrides: { [TASK_PARAM.raiser]: "" },
    });
  }
  if (deadlineSpan) {
    activeChips.push({
      key: TASK_PARAM.deadlineFrom,
      label: `${deadlineLabel}: ${deadlineSpan}`,
      overrides: { [TASK_PARAM.deadlineFrom]: "", [TASK_PARAM.deadlineTo]: "" },
    });
  }
  if (raisedSpan) {
    activeChips.push({
      key: TASK_PARAM.raisedFrom,
      label: `${raisedLabel}: ${raisedSpan}`,
      overrides: { [TASK_PARAM.raisedFrom]: "", [TASK_PARAM.raisedTo]: "" },
    });
  }

  return (
    <div
      className={`lt-fbar lt-fsheet-host${isPending ? " wfbusy" : ""}`}
      role="group"
      aria-label={copy(pageContract, "filter.bar_aria")}
      aria-busy={isPending || undefined}
    >
      <span className="lt-fsearch" title={searchHint}>
        <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
        <input
          type="search"
          value={text}
          onChange={(event) => onSearchChange(event.target.value)}
          placeholder={searchLabel}
          aria-label={searchHint}
          autoComplete="off"
          maxLength={120}
        />
        {text ? (
          <button
            type="button"
            className="lt-qclr"
            aria-label={copy(pageContract, "action.clear")}
            onClick={() => {
              setText("");
              if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
              go(paramsWith({ [TASK_PARAM.q]: "" }));
            }}
          >
            <X className="ic" style={{ width: 13 }} aria-hidden="true" />
          </button>
        ) : null}
      </span>

      {/* The phone affordance. Hidden on a wide viewport, where the same controls are the bar. */}
      <button
        type="button"
        ref={moreRef}
        className="btn sm lt-fmore"
        aria-expanded={sheetOpen}
        onClick={() => (sheetOpen ? closeSheet() : setSheetOpen(true))}
      >
        <SlidersHorizontal className="ic" aria-hidden="true" />
        {copy(pageContract, "action.filters")}
      </button>

      {sheetOpen ? (
        <button
          type="button"
          className="scrim on lt-fscrim"
          aria-label={copy(pageContract, "filter.close_label")}
          onClick={closeSheet}
        />
      ) : null}

      <div ref={sheetRef} className={`lt-fgroup${sheetOpen ? " open" : ""}`} tabIndex={-1}>
        <div className="lt-fsheet-hd">
          <ListFilter className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <b>{copy(pageContract, "action.filters")}</b>
          <span className="sp" style={{ flex: 1 }} />
          <button
            type="button"
            className="btn sm"
            onClick={closeSheet}
            aria-label={copy(pageContract, "filter.close_label")}
          >
            <X className="ic" aria-hidden="true" />
          </button>
        </div>

        {/* ONE segmented group, not a row of floating pills: these are alternatives (exactly one is
            current), and a segment is how the rest of this app says so -- see `.metricseg`. */}
        <div className="lt-chips lt-seg" role="group" aria-label={copy(pageContract, "filter.status_aria", copy(pageContract, "column.status", "Status"))}>
          {statusChips.map((chip) => (
            <Link
              key={chip.key}
              href={hrefWith({ [TASK_PARAM.filter]: chip.key })}
              replace
              scroll={false}
              className={chipSelected(chip.key) ? "on" : ""}
              // The overdue chip carries the deadline pill's danger tone (`.lt-clock-late`): it is
              // the one chip that names a problem rather than a stage.
              data-filter={chip.key}
              aria-current={chipSelected(chip.key) ? "true" : undefined}
              // A real link with a real href, so open-in-new-tab and sharing still work; the
              // handler only takes over the PLAIN click, to light the segment up before the server
              // answers. Modified clicks are left to the browser.
              onClick={(event) => {
                if (
                  event.metaKey ||
                  event.ctrlKey ||
                  event.shiftKey ||
                  event.altKey ||
                  event.button !== 0
                ) {
                  return;
                }
                event.preventDefault();
                go(paramsWith({ [TASK_PARAM.filter]: chip.key }));
              }}
            >
              {chip.label}
              <b>{chip.count}</b>
            </Link>
          ))}
        </div>

        {/* THE TWO PERSON FILTERS, both searchable. These were native `<select>`s, and the board
            carried a THIRD spelling of `t_assignee` as an avatar group whose `+5` overflow was a
            dead `<span aria-hidden>`. `task-people-filter.tsx` is now the only person filter on
            this desk; see that file for why the three converged into one. */}
        {/* `.lt-fslot` is the bar's PERSON-PICKER slot: a `display:contents` container the
            toolbar owns, so whichever picker component sits in it (this one, or the shared
            Work Board assignee picker it is due to be replaced by) inherits the bar's row and
            the sheet's column without the picker knowing either. */}
        {/* The two slots host the Work Board's OWN assignee picker (`components/assignee-picker.tsx`,
            multi mode: the avatar stack, `+N`, one pick, per-person counts on this page, the
            "All" foot) so the two toolbars are one control language. A scope that already pins
            the person renders NO picker, as the Work Board drops its stack for `ownRowsOnly`.
            The `wb` class on the slot scopes the board's own `.avs` rules onto it verbatim. */}
        <div className="lt-fslot wb" data-slot="assignee" data-key={assigneeLabel} title={assigneePinned ? copy(pageContract, "filter.assignee_pinned") : assigneeLabel}>
          {assigneePinned ? null : (
            <AssigneePicker
              mode="multi"
              labels={{ label: assigneeLabel, search: peopleSearchLabel, none: peopleNoMatchesLabel, selectAll: allOption, rows: onThisPageLabel }}
              owners={assigneeOptions.map((option) => ({ id: option.value, name: option.label, title: option.title }))}
              cardsByOwner={assigneeCounts}
              selected={fieldValue(TASK_PARAM.assignee, assignee) || undefined}
              onSelect={(next) => {
                closeSheet();
                go(paramsWith({ [TASK_PARAM.assignee]: next ?? "" }));
              }}
            />
          )}
        </div>

        <div className="lt-fslot wb" data-slot="raiser" data-key={raiserLabel} title={raiserPinned ? copy(pageContract, "filter.raiser_pinned") : raiserLabel}>
          {raiserPinned ? null : (
            <AssigneePicker
              mode="multi"
              labels={{ label: raiserLabel, search: peopleSearchLabel, none: peopleNoMatchesLabel, selectAll: allOption, rows: onThisPageLabel }}
              owners={raiserOptions.map((option) => ({ id: option.value, name: option.label, title: option.title }))}
              cardsByOwner={raiserCounts}
              selected={fieldValue(TASK_PARAM.raiser, raiser) || undefined}
              onSelect={(next) => {
                closeSheet();
                go(paramsWith({ [TASK_PARAM.raiser]: next ?? "" }));
              }}
            />
          )}
        </div>

        {/* "Sort · Newest first": the label is INSIDE the control, as it is for the two people
            pickers and the two date disclosures. The bar had three label treatments in one row
            (floating text beside a pill, a floating label beside a native select, and a bare
            disclosure); `.lt-fkey` is the one treatment now. */}
        <label className="lt-fsel">
          <span className="lt-fkey">{copy(pageContract, "filter.sort")}</span>
          <select value={fieldValue(TASK_PARAM.sort, sort)} onChange={(event) => go(paramsWith({ [TASK_PARAM.sort]: event.target.value }))}>
            {TASK_SORTS.map((option) => (
              <option key={option} value={option}>
                {copy(pageContract, `sort.${option}`)}
              </option>
            ))}
          </select>
        </label>

        {/* THE DATE SPANS, behind ONE compact disclosure -- the Work Board's toolbar has one date
            control, and this bar now has one too. It STATES what is applied ("Dates · any", or
            "Dates · Deadline 01/09/2026 – 30/09/2026 · Raised …") without being opened; inside,
            the two spans sit as two labelled groups, each committing its OWN span whole on one
            press (half a span is a 400 `invalid_date_range` on this endpoint, so these are the one
            set of controls here that cannot apply on change -- every other control does). */}
        <div className="lt-franges" ref={rangesRef}>
          {(() => {
            const ranges = [
              {
                id: "deadline" as const,
                label: deadlineLabel,
                span: deadlineSpan,
                fromLabel: copy(pageContract, "filter.deadline_from"),
                toLabel: copy(pageContract, "filter.deadline_to"),
                from: dates.deadlineFrom,
                to: dates.deadlineTo,
                setFrom: (value: string) => setDates((prev) => ({ ...prev, deadlineFrom: value })),
                setTo: (value: string) => setDates((prev) => ({ ...prev, deadlineTo: value })),
                commit: (from: string, to: string) => ({
                  [TASK_PARAM.deadlineFrom]: from,
                  [TASK_PARAM.deadlineTo]: to,
                }),
              },
              {
                id: "raised" as const,
                label: raisedLabel,
                span: raisedSpan,
                fromLabel: copy(pageContract, "filter.raised_from"),
                toLabel: copy(pageContract, "filter.raised_to"),
                from: dates.raisedFrom,
                to: dates.raisedTo,
                setFrom: (value: string) => setDates((prev) => ({ ...prev, raisedFrom: value })),
                setTo: (value: string) => setDates((prev) => ({ ...prev, raisedTo: value })),
                commit: (from: string, to: string) => ({
                  [TASK_PARAM.raisedFrom]: from,
                  [TASK_PARAM.raisedTo]: to,
                }),
              },
            ] as const;
            const applied = ranges.filter((range) => range.span);
            const stated = applied.length
              ? applied.map((range) => `${range.label} ${range.span}`).join(" · ")
              : anyLabel;
            return (
              <div className="lt-fdrop">
                <button
                  type="button"
                  className={applied.length ? "set" : ""}
                  aria-expanded={openRange === "dates"}
                  aria-haspopup="true"
                  onClick={() => setOpenRange((current) => (current === "dates" ? null : "dates"))}
                >
                  <CalendarRange className="ic" style={{ width: 14 }} aria-hidden="true" />
                  <span className="lt-fkey">{datesLabel}</span>
                  <span>{stated}</span>
                  <ChevronDown className="ic" style={{ width: 13 }} aria-hidden="true" />
                </button>
                {openRange === "dates" ? (
                  <div className="lt-fdrop-pop lt-fdrop-dates" role="group" aria-label={datesLabel}>
                    {ranges.map((range) => (
                      <div className="lt-fdrop-range" role="group" aria-label={range.label} key={range.id}>
                        <span className="lt-fdrop-rangekey">{range.label}</span>
                        <label className="lt-fsel">
                          <span>{range.fromLabel}</span>
                          <input
                            type="date"
                            value={range.from}
                            onChange={(event) => range.setFrom(event.target.value)}
                          />
                        </label>
                        <label className="lt-fsel">
                          <span>{range.toLabel}</span>
                          <input
                            type="date"
                            value={range.to}
                            onChange={(event) => range.setTo(event.target.value)}
                          />
                        </label>
                        <div className="lt-fdrop-act">
                          {range.span ? (
                            <button
                              type="button"
                              className="btn sm"
                              onClick={() => {
                                range.setFrom("");
                                range.setTo("");
                                setOpenRange(null);
                                closeSheet();
                                go(paramsWith(range.commit("", "")));
                              }}
                            >
                              {clearLabel}
                            </button>
                          ) : null}
                          <button
                            type="button"
                            className="btn sm p"
                            onClick={() => {
                              setOpenRange(null);
                              closeSheet();
                              go(paramsWith(range.commit(range.from, range.to)));
                            }}
                          >
                            {applyLabel}
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          })()}
        </div>

        {/* WHAT IS NARROWING THE LIST, each chip removing exactly itself. */}
        {activeChips.length > 0 ? (
          <div className="lt-factive" role="group" aria-label={copy(pageContract, "filter.active_aria", copy(pageContract, "action.filters", "Filters"))}>
            {activeChips.map((chip) => (
              <span className="achip" key={chip.key}>
                {chip.label}
                <button
                  type="button"
                  onClick={() => {
                    // The search box is the one chip with local state behind it, so its pending
                    // debounce is dropped here rather than being left to fire the old value back.
                    if (chip.key === TASK_PARAM.q) {
                      setText("");
                      if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
                    }
                    go(paramsWith(chip.overrides));
                  }}
                  aria-label={removeLabel}
                  title={removeLabel}
                >
                  <X className="ic" style={{ width: 12 }} aria-hidden="true" />
                </button>
              </span>
            ))}
          </div>
        ) : null}

        {hasFilters ? (
          <Link href={clearedHref} replace scroll={false} className="achip clr lt-fclear">
            {clearLabel}
          </Link>
        ) : null}

        {rangeIncomplete ? <span className="lt-fnote">{rangeNote}</span> : null}
      </div>
    </div>
  );
}
