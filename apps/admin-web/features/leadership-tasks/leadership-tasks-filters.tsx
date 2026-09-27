"use client";

import Link from "@/components/no-prefetch-link";
import { useCallback, useEffect, useMemo, useRef, useState, useTransition } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { usePopover } from "minimal-shared/hooks";
import { varAlpha } from "minimal-shared/utils";

import Box from "@mui/material/Box";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import Chip from "@mui/material/Chip";
import Button from "@mui/material/Button";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import InputAdornment from "@mui/material/InputAdornment";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { chipProps } from "@/components/minimal/filters-result";
import { TaskPeopleDropdown, type TaskPeopleOption } from "@/components/people-dropdown";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { worklistFilterShownValue } from "@/lib/worklist-filter-value";
import { TASK_PAGING_PARAMS, TASK_PARAM } from "./params";
import type { TaskRow } from "./task-row";
import { taskRowPatch, useTaskRowsVersion } from "./task-row-store";
import { patchApplies } from "./task-detail-pick";
import { dateSpanLabel, personFilterLabel } from "./filter-labels";
import { DEFAULT_TASK_SORT, TASK_SORTS, type TaskSort } from "./task-url";


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
 * The Tasks toolbar: the status tabs, the two people pickers, sort, free text and both date
 * spans, in the template list anatomy (sections/user/view/user-list-view.tsx card head +
 * sections/user/user-table-toolbar.tsx).
 *
 * WHY THIS IS NOT `components/worklist-filters.tsx`: that component has no debounced text-search
 * kind, and its date kinds write ONE end of a span per change -- this endpoint answers 400
 * `invalid_date_range` for half a span -- so both spans sit behind one disclosure with one Apply.
 * Every other control applies on change. Every string comes from the page contract.
 *
 * At phone width the toolbar stacks in flow (template xs column); every popup it opens is a
 * portalled MUI surface, so there is no page-owned overlay to trap focus or scroll.
 */
/** `a,b,c` -> ids; the toolbar's people filters are checkboxes and the URL carries every tick. */
function splitIDs(raw: string): string[] {
  return raw.split(",").map((x) => x.trim()).filter(Boolean);
}

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
  rows = [],
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
  /** The page's rows as the server sent them; the chips adjust their counts by what the row store moved. */
  rows?: readonly TaskRow[];
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
  /** The ONE dates disclosure (both spans inside it): the template menu popover. */
  const datesPopover = usePopover();
  const rangesRef = useRef<HTMLDivElement>(null);
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

  // The chip counts are whole-list server numbers; a status changed in the drawer moves ONE row
  // between them, and the row store knows which. Same arithmetic the board's column pills use:
  // +1 into the new status, -1 out of the old, and the overdue lens counts only late WORKING rows.
  useTaskRowsVersion();
  const chipDelta = (key: string): number => {
    let delta = 0;
    for (const row of rows) {
      const patch = taskRowPatch(row.id);
      // An older patch than the server row is stale and moved nothing (the board's fence).
      const after = patchApplies(row.rowVersion, patch) ? (patch?.status ?? row.status) : row.status;
      if (after === row.status) continue;
      const late = row.daysLeft !== null && row.daysLeft < 0;
      const working = (status: string) => status === "open" || status === "in_progress";
      if (key === "overdue") {
        if (late && working(after) && !working(row.status)) delta += 1;
        if (late && working(row.status) && !working(after)) delta -= 1;
        continue;
      }
      if (after === key) delta += 1;
      if (row.status === key) delta -= 1;
    }
    return delta;
  };
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
  const datesLabel = copy(pageContract, "filter.dates", "Dates");

  /**
   * A span as the disclosure's own label: the reader should not have to open it to learn whether
   * it is narrowing the list. Shown as DD/MM/YYYY (`dateSpanLabel`); the URL keeps ISO dates.
   */
  const deadlineSpan = dateSpanLabel(deadlineFrom, deadlineTo);
  const raisedSpan = dateSpanLabel(raisedFrom, raisedTo);

  const shownSortRaw = fieldValue(TASK_PARAM.sort, sort);
  const shownSort: TaskSort = (TASK_SORTS as readonly string[]).includes(shownSortRaw) ? (shownSortRaw as TaskSort) : DEFAULT_TASK_SORT;
  const shownAssignee = fieldValue(TASK_PARAM.assignee, assignee);
  const shownRaiser = fieldValue(TASK_PARAM.raiser, raiser);
  // One name, or "Dinakar, Manju" for a multi-tick; an unknown id reads as a neutral label, never
  // the raw id.
  const unknownPerson = copy(pageContract, "filter.unknown_person", "Unknown person");
  const labelFor = (options: TaskPersonOption[], value: string) =>
    personFilterLabel(options, splitIDs(value), unknownPerson);
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


  // The date spans, both committed together by ONE Apply (half a span is a 400
  // `invalid_date_range` on this endpoint, so these are the one set of controls here that cannot
  // apply on change -- every other control does). ONE Clear drops both.
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
    },
  ] as const;
  const appliedRanges = ranges.filter((range) => range.span);
  const datesStated = appliedRanges.length
    ? appliedRanges.map((range) => `${range.label} ${range.span}`).join(" · ")
    : anyLabel;
  const commitAll = (next: typeof dates) => ({
    [TASK_PARAM.deadlineFrom]: next.deadlineFrom,
    [TASK_PARAM.deadlineTo]: next.deadlineTo,
    [TASK_PARAM.raisedFrom]: next.raisedFrom,
    [TASK_PARAM.raisedTo]: next.raisedTo,
  });
  const anyPending = Boolean(dates.deadlineFrom || dates.deadlineTo || dates.raisedFrom || dates.raisedTo);
  const previousMonthLabel = copy(pageContract, "date.previous_month", "Previous month");
  const nextMonthLabel = copy(pageContract, "date.next_month", "Next month");
  const clearSearch = () => {
    setText("");
    if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
  };

  // Template list card head (sections/user/view/user-list-view.tsx): the status Tabs with Label
  // counts, the toolbar (sections/user/user-table-toolbar.tsx: outlined multi-select filters, the
  // keyword field with a search adornment), then the applied-filter chips with one Clear
  // (sections/user/user-table-filters-result.tsx anatomy). Every control applies on change.
  return (
    <Box
      role="group"
      aria-label={copy(pageContract, "filter.bar_aria")}
      aria-busy={isPending || undefined}
    >
      {/* ONE status group: alternatives, exactly one current -- the template's status tabs. */}
      <Tabs
        value={selectedChipKey || "all"}
        variant="scrollable"
        scrollButtons="auto"
        allowScrollButtonsMobile
        aria-label={copy(pageContract, "filter.status_aria", copy(pageContract, "column.status", "Status"))}
        sx={[
          (theme) => ({
            px: { md: 2.5 },
            boxShadow: `inset 0 -2px 0 0 ${varAlpha(theme.vars.palette.grey["500Channel"], 0.08)}`,
          }),
        ]}
      >
        {statusChips.map((chip) => {
          const on = chipSelected(chip.key);
          return (
            <Tab
              key={chip.key}
              value={chip.key}
              component={Link}
              href={hrefWith({ [TASK_PARAM.filter]: chip.key })}
              replace
              scroll={false}
              data-filter={chip.key}
              aria-current={on ? "true" : undefined}
              iconPosition="end"
              label={chip.label}
              icon={
                <Label variant={chip.key === "all" || on ? "filled" : "soft"} color={chipColor(chip.key)}>
                  {Math.max(0, chip.count + chipDelta(chip.key))}
                </Label>
              }
              onClick={(event: React.MouseEvent<HTMLElement>) => {
                if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) {
                  return;
                }
                event.preventDefault();
                go(paramsWith({ [TASK_PARAM.filter]: chip.key }));
              }}
            />
          );
        })}
      </Tabs>

      <Box
        sx={{
          p: 2.5,
          gap: 2,
          display: "flex",
          pr: { xs: 2.5, md: 1 },
          flexDirection: { xs: "column", md: "row" },
          alignItems: { xs: "stretch", md: "center" },
          opacity: isPending ? 0.8 : 1,
        }}
      >
        {/* THE TWO PERSON FILTERS. A scope that already pins the person renders none (Gate-1 #7):
            the scope tab already says whose tasks these are. */}
        {assigneePinned ? null : (
          <TaskPeopleDropdown
            slot="assignee"
            label={assigneeLabel}
            allLabel={allOption}
            options={assigneeOptions.map((option) => ({ id: option.value, name: option.label, title: option.title }))}
            selected={splitIDs(fieldValue(TASK_PARAM.assignee, assignee))}
            onChange={(next) => {
              go(paramsWith({ [TASK_PARAM.assignee]: next.join(",") }));
            }}
          />
        )}
        {raiserPinned ? null : (
          <TaskPeopleDropdown
            slot="raiser"
            label={raiserLabel}
            allLabel={allOption}
            options={raiserOptions.map((option) => ({ id: option.value, name: option.label, title: option.title }))}
            selected={splitIDs(fieldValue(TASK_PARAM.raiser, raiser))}
            onChange={(next) => {
              go(paramsWith({ [TASK_PARAM.raiser]: next.join(",") }));
            }}
          />
        )}

        {/* Sort: an outlined select like the person filters. An unknown `t_sort` (stale link)
            shows the default the server applies, never a blank. */}
        <TextField
          select
          label={copy(pageContract, "filter.sort")}
          value={shownSort}
          onChange={({ target: { value } }) => go(paramsWith({ [TASK_PARAM.sort]: value }))}
          sx={{ flexShrink: 0, width: { xs: 1, md: 180 } }}
          slotProps={{ select: { MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          {TASK_SORTS.map((option) => (
            <MenuItem key={option} value={option}>
              {copy(pageContract, `sort.${option}`)}
            </MenuItem>
          ))}
        </TextField>

        {/* Phone: the search takes the full row and "Dates" wraps under it, so the field is never
            squeezed to a truncated placeholder beside the button. guard: tasks-phone-search-row */}
        <Box sx={{ gap: 2, width: 1, flexGrow: 1, display: "flex", flexWrap: { xs: "wrap", sm: "nowrap" }, alignItems: "center", minWidth: 0 }}>
          <TextField
            fullWidth
            type="search"
            value={text}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder={searchLabel}
            title={searchHint}
            slotProps={{
              htmlInput: { "aria-label": searchHint, autoComplete: "off", maxLength: 120 },
              input: {
                startAdornment: (
                  <InputAdornment position="start">
                    <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
                  </InputAdornment>
                ),
                endAdornment: text ? (
                  <InputAdornment position="end">
                    <IconButton
                      edge="end"
                      aria-label={clearLabel}
                      onClick={() => {
                        clearSearch();
                        go(paramsWith({ [TASK_PARAM.q]: "" }));
                      }}
                    >
                      <Iconify icon="mingcute:close-line" width={18} />
                    </IconButton>
                  </InputAdornment>
                ) : null,
              },
            }}
          />

          {/* THE DATE SPANS, behind ONE disclosure that states what is applied ("Dates · any") without
              being opened. The four fields are the console's calendar (`ThemedDatePicker`). */}
          <Button
            color="inherit"
            onClick={datesPopover.onOpen}
            aria-expanded={datesPopover.open}
            aria-haspopup="true"
            startIcon={<Iconify icon="solar:calendar-date-bold" />}
            endIcon={<Iconify icon={datesPopover.open ? "eva:arrow-ios-upward-fill" : "eva:arrow-ios-downward-fill"} />}
            sx={{ flexShrink: 0, fontWeight: "fontWeightSemiBold", maxWidth: { xs: 1, md: 320 } }}
          >
            {datesLabel}:
            <Box component="span" sx={{ ml: 0.5, fontWeight: "fontWeightBold", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
              {datesStated}
            </Box>
          </Button>
        </Box>
      </Box>

      <CustomPopover
        open={datesPopover.open}
        anchorEl={datesPopover.anchorEl}
        onClose={datesPopover.onClose}
        slotProps={{ arrow: { placement: "top-right" }, paper: { sx: { p: 2, width: (theme) => theme.spacing(45), maxWidth: "calc(100vw - var(--sp-4))" } } }}
      >
        <Box
          ref={rangesRef}
          role="group"
          aria-label={datesLabel}
          sx={{ display: "flex", flexDirection: "column", gap: 2 }}
          onKeyDownCapture={(event) => {
            // An open calendar inside a date field takes the first Escape; the popover the second.
            if (event.key !== "Escape") return;
            const openCalendar = rangesRef.current?.querySelector("details[open]");
            if (!openCalendar) return;
            event.preventDefault();
            event.stopPropagation();
            (openCalendar as HTMLDetailsElement).open = false;
            (openCalendar.querySelector("summary") as HTMLElement | null)?.focus();
          }}
        >
          {ranges.map((range) => (
            <Box key={range.id} role="group" aria-label={range.label} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <Typography variant="subtitle2">{range.label}</Typography>
              <Box sx={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 1.5 }}>
                <ThemedDatePicker
                  name={`${range.id}_from`}
                  label={range.fromLabel}
                  cleared={anyLabel}
                  value={range.from}
                  onChange={range.setFrom}
                  max={range.to || undefined}
                  previousMonthLabel={previousMonthLabel}
                  nextMonthLabel={nextMonthLabel}
                  invalidDateText=""
                />
                <ThemedDatePicker
                  name={`${range.id}_to`}
                  label={range.toLabel}
                  cleared={anyLabel}
                  value={range.to}
                  onChange={range.setTo}
                  min={range.from || undefined}
                  previousMonthLabel={previousMonthLabel}
                  nextMonthLabel={nextMonthLabel}
                  invalidDateText=""
                />
              </Box>
            </Box>
          ))}
          <Box sx={{ display: "flex", justifyContent: "flex-end", gap: 1 }}>
            {appliedRanges.length || anyPending ? (
              <Button
                variant="outlined"
                color="inherit"
                onClick={() => {
                  const cleared = { deadlineFrom: "", deadlineTo: "", raisedFrom: "", raisedTo: "" };
                  setDates(cleared);
                  datesPopover.onClose();
                  go(paramsWith(commitAll(cleared)));
                }}
              >
                {clearLabel}
              </Button>
            ) : null}
            <Button
              variant="contained"
              color="primary"
              onClick={() => {
                datesPopover.onClose();
                go(paramsWith(commitAll(dates)));
              }}
            >
              {applyLabel}
            </Button>
          </Box>
        </Box>
      </CustomPopover>

      {/* WHAT IS NARROWING THE LIST, each chip removing exactly itself, on a row of its own under
          the toolbar (Gate-1 #9), with ONE Clear. */}
      {activeChips.length > 0 || hasFilters || rangeIncomplete ? (
        <Box sx={{ p: 2.5, pt: 0, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
          {activeChips.length > 0 ? (
            <Box
              role="group"
              aria-label={copy(pageContract, "filter.active_aria", copy(pageContract, "action.filters", "Filters"))}
              sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}
            >
              {activeChips.map((chip) => (
                <Chip
                  {...chipProps}
                  key={chip.key}
                  label={chip.label}
                  title={removeLabel}
                  deleteIcon={<Iconify icon="solar:close-circle-bold" role="button" aria-label={removeLabel} />}
                  onDelete={() => {
                    if (chip.key === TASK_PARAM.q) clearSearch();
                    go(paramsWith(chip.overrides));
                  }}
                />
              ))}
            </Box>
          ) : null}
          {hasFilters ? (
            <Button component={Link} href={clearedHref} replace scroll={false} color="error" startIcon={<Iconify icon="solar:trash-bin-trash-bold" />}>
              {clearLabel}
            </Button>
          ) : null}
          {rangeIncomplete ? (
            <Typography variant="caption" sx={{ color: "warning.main", width: 1 }}>
              {rangeNote}
            </Typography>
          ) : null}
        </Box>
      ) : null}
    </Box>
  );
}

/** Template status-tab Label colours: the same tone a status reads everywhere else on the desk. */
function chipColor(key: string): "default" | "warning" | "info" | "success" | "error" {
  if (key === "open") return "warning";
  if (key === "in_progress") return "info";
  if (key === "done") return "success";
  if (key === "overdue") return "error";
  return "default";
}
