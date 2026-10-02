"use client";
import Table from "@mui/material/Table";
import { Scrollbar } from "@/components/minimal/scrollbar";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useRouter } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import type { HerdSignalItem, HerdSignalsTagMappingResponse } from "@/lib/api/herd-signals";
import { MAPPING_LABEL, MAPPING_TONE, fmtBleMac, fmtAgo } from "./format";
import { useHerdSignalsNav } from "./herd-signals-nav-context";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { herdSignalsHref, type HerdSignalsParams } from "./params";
import { EmptyState } from "@/components/app/empty-state";
import { TableSkeleton } from "@/components/app/skeletons";
import Box from "@mui/material/Box";
import Alert from "@mui/material/Alert";
import Paper from "@mui/material/Paper";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import DialogTitle from "@mui/material/DialogTitle";
import ListItemText from "@mui/material/ListItemText";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import InputAdornment from "@mui/material/InputAdornment";
import ListItemButton from "@mui/material/ListItemButton";
import CircularProgress from "@mui/material/CircularProgress";
import { Iconify } from "@/components/minimal/iconify";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";
import { HS_FAINT, HS_MONO, HS_SUBLINE, respTableSx, selectableRowSx } from "./herd-signals-sx";

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
// the page size the reader had chosen (the default LIMIT_DEFAULT 25 is one of them, or the pager
// select shows blank).
const PAGE_SIZE_OPTIONS = [25, 50, 100];

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
        <Box component="b" sx={HS_FAINT}>{NOT_ON_CONTRACT}</Box>
        <Box component="small" sx={HS_SUBLINE}>No animal identifier</Box>
      </>
    );
  }
  return (
    <>
      <b>{item.display_id}</b>
      <Box component="small" sx={HS_SUBLINE}>{MAPPING_LABEL[item.mapping_state].toLowerCase()}</Box>
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

  const secondary = (animal: AnimalOption) =>
    `${animal.animal_identifier_1 ? `Tag ${animal.animal_identifier_1}` : "No ear-tag value on record"}${animal.breed ? ` · ${animal.breed}` : ""}`;

  // Template search field + outlined result list (sections/_examples list + TextField search pattern).
  return (
    <Stack spacing={1}>
      <TextField
        id="hs-animal-q"
        type="search"
        label="Animal"
        placeholder="Search by animal ID or ear-tag value"
        value={q}
        onChange={(event) => runSearch(event.target.value)}
        autoComplete="off"
        fullWidth
        slotProps={{
          inputLabel: { shrink: true },
          input: {
            startAdornment: (
              <InputAdornment position="start">
                <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
              </InputAdornment>
            ),
            endAdornment:
              state === "loading" ? (
                <InputAdornment position="end">
                  <CircularProgress size={16} color="inherit" aria-hidden />
                </InputAdornment>
              ) : null,
          },
        }}
      />

      {selected ? (
        <Paper variant="outlined" sx={{ display: "flex", alignItems: "center", gap: 1.5, px: 1.5, py: 1, borderColor: "primary.main", bgcolor: "action.selected" }}>
          <ListItemText primary={selected.display_id} secondary={secondary(selected)} slotProps={{ primary: { variant: "subtitle2" } }} sx={{ m: 0, minWidth: 0 }} />
          <Button size="small" variant="outlined" color="inherit" onClick={() => onSelect(null)}>
            Change
          </Button>
        </Paper>
      ) : (
        <Box role="listbox" aria-label="Animal search results" sx={{ maxHeight: 210, overflowY: "auto", display: "flex", flexDirection: "column", gap: 0.5 }}>
          {q.trim().length < 2 ? (
            <Typography variant="body2" sx={{ color: "text.disabled" }}>Type at least two characters of an animal ID or ear-tag value.</Typography>
          ) : state === "error" ? (
            <Typography variant="body2" sx={{ color: "error.main" }}>{error}</Typography>
          ) : state === "loading" ? (
            <Typography variant="body2" sx={{ color: "text.disabled" }}>Searching…</Typography>
          ) : results.length === 0 ? (
            <Typography variant="body2" sx={{ color: "text.disabled" }}>No living animal matches that search.</Typography>
          ) : (
            results.map((animal) => (
              <ListItemButton
                key={animal.goat_id}
                role="option"
                aria-selected={false}
                onClick={() => onSelect(animal)}
                divider
                sx={{ flex: "none" }}
              >
                <ListItemText primary={animal.display_id} secondary={secondary(animal)} slotProps={{ primary: { variant: "subtitle2" } }} sx={{ m: 0 }} />
              </ListItemButton>
            ))
          )}
        </Box>
      )}

      {/* What the chosen animal ALREADY carries. This is the whole point of probing before submit:
          an animal that already has a live smart tag needs REPLACE, not MAP. */}
      {selected ? (
        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {liveTagsState === "loading"
            ? "Checking what this animal already carries…"
            : liveTagsState === "error"
              ? "Could not check what this animal already carries — the write will still be checked by the server."
              : liveTags.length === 0
                ? "This animal carries no live smart tag."
                : `This animal already carries ${liveTags.length === 1 ? "a live smart tag" : `${liveTags.length} live smart tags`}: ${liveTags.map((tag) => tag.tag_id).join(", ")}.`}
        </Typography>
      ) : null}
    </Stack>
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
  const [animal, setAnimal] = useState<AnimalOption | null>(null);
  const [liveTags, setLiveTags] = useState<LiveSmartTag[]>([]);
  const [liveTagsState, setLiveTagsState] = useState<"unasked" | "loading" | "ready" | "error">("unasked");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const close = useCallback(() => {
    if (submitting) return;
    onClose();
  }, [onClose, submitting]);

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

  // Template custom-dialog (ConfirmDialog) layout on MUI Dialog: portals, traps focus, restores it
  // to the row action on close, and Escape / scrim close it (blocked while the write is in flight).
  return (
    <Dialog
      open
      fullWidth
      maxWidth="sm"
      onClose={close}
      aria-labelledby="hs-mapping-title"
      slotProps={{ paper: { "aria-label": ACTION_TITLE[action], "aria-busy": submitting } }}
    >
      <DialogTitle id="hs-mapping-title" component="div" sx={{ display: "flex", alignItems: "flex-start", gap: 1, pb: 1 }}>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography variant="h6" component="h3">{ACTION_TITLE[action]}</Typography>
          <Typography variant="body2" sx={{ color: "text.secondary", mt: 0.25 }}>
            BLE tag <Box component="span" sx={{ fontFamily: "monospace" }}>{item.tag_id}</Box>
            {item.tag_mac ? (
              <>
                {" · MAC "}
                <Box component="span" sx={{ fontFamily: "monospace" }}>{fmtBleMac(item.tag_mac)}</Box>
              </>
            ) : null}
          </Typography>
        </Box>
        <IconButton onClick={close} disabled={submitting} aria-label="Close">
          <Iconify icon="mingcute:close-line" />
        </IconButton>
      </DialogTitle>

      <DialogContent dividers sx={{ display: "flex", flexDirection: "column", gap: 1.5, pt: 1 }}>
        {needsAnimal ? (
          <AnimalPicker selected={animal} onSelect={setAnimal} liveTags={liveTags} liveTagsState={liveTagsState} />
        ) : (
          <Typography variant="body2">
            {item.display_id
              ? `This releases the binding between ${item.tag_id} and ${item.display_id}.`
              : `This releases the binding for ${item.tag_id}.`}
          </Typography>
        )}

        {/* The monitoring boundary (backend/migrations/postgres 000196). Mapping stamps the
            instant this animal's monitoring begins; every packet the tag sent before it stays
            device telemetry and never enters this animal's baseline, pattern window, or
            correlations. The operator must not read a fresh mapping as inherited record. */}
        <Alert severity="info" variant="outlined" icon={false} sx={{ typography: "caption" }}>
          {action === "unmap"
            ? "This tag keeps broadcasting and nothing already stored is deleted — it simply stops being attributed to an animal from now on, and no further animal-attributed value is produced for it."
            : "Monitoring for this animal starts at the moment you confirm. Everything this tag broadcast earlier stays device telemetry and never becomes part of this animal's record."}
        </Alert>

        {error ? (
          <Alert severity="error" role="alert">
            {error}
          </Alert>
        ) : null}
      </DialogContent>

      <DialogActions sx={{ flexWrap: "wrap", gap: 1 }}>
        {blockedReason && !error ? (
          <Typography variant="caption" sx={{ color: "text.disabled", mr: "auto" }}>{blockedReason}</Typography>
        ) : null}
        <Button variant="outlined" color="inherit" onClick={close} disabled={submitting}>
          Cancel
        </Button>
        <Button
          variant="contained" color="primary"
          onClick={submit}
          disabled={submitting || blockedReason !== null}
          title={blockedReason ?? undefined}
          aria-busy={submitting}
          loading={submitting}
          loadingPosition="start"
        >
          {submitting ? "Working…" : action === "map" ? "Map to animal" : action === "replace" ? "Replace tag" : "Unmap"}
        </Button>
      </DialogActions>
    </Dialog>
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
      // A disabled button swallows hover, so the reason rides on a wrapping span's title.
      <Box component="span" title={reason ?? ACTION_TITLE[action]} sx={{ display: "inline-flex" }}>
        <Button
          size="small"
          variant={primary ? "contained" : "outlined"}
          color={primary ? "primary" : "inherit"}
          disabled={reason !== null || busy}
          onClick={() => {
            setFlash(null);
            setDialog(action);
          }}
        >
          {label}
        </Button>
      </Box>
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

  const rangeLabel = `Showing ${nf(rangeFrom)}–${nf(rangeTo)}${total ? ` of ${nf(total)}` : ""} · page ${nf(pageIndex + 1)}${pageCount ? ` of ${nf(pageCount)}` : ""}`;
  // Template table pager (TablePaginationLinks: TablePagination anatomy, prev / next as links, rows
  // per page navigating to a prepared href), above and below the rows as the mock draws it.
  const pager = (variant: "top" | "bottom") => (
    <Box aria-busy={busy} sx={variant === "top" ? { borderBottom: 1, borderColor: "divider" } : undefined}>
      <TablePaginationLinks
        page={pageIndex}
        rowsPerPage={params.limit}
        count={-1}
        rowsPerPageHrefs={PAGE_SIZE_OPTIONS.map((size) => ({ value: size, href: herdSignalsHref(params, { hs_limit: String(size) }) }))}
        prevHref={prevHref}
        nextHref={nextHref}
        onPrevClick={goPrev}
        onNextClick={goNext}
        labelRowsPerPage="Rows per page"
        rangeLabel={rangeLabel}
        prevLabel="Previous page"
        nextLabel="Next page"
        left={busy ? <CircularProgress size={16} color="inherit" aria-hidden="true" title="Loading" sx={{ color: "text.secondary" }} /> : null}
      />
    </Box>
  );

  const filterButton = (label: string, value: HerdSignalsParams["mappingState"], selected: boolean) => {
    const href = herdSignalsHref(params, { hs_map: value });
    return (
      <Button
        component={Link}
        href={href}
        size="small"
        variant={selected ? "contained" : "outlined"}
        color={selected ? "primary" : "inherit"}
        aria-current={selected ? "true" : undefined}
        onClick={(event: React.MouseEvent) => {
          if (!plainClick(event)) return;
          event.preventDefault();
          navigate(href);
        }}
      >
        {label}
      </Button>
    );
  };

  // Template list toolbar row inside the card: the mapping-state filters, the selection readout and
  // the three write actions.
  const toolbar = (
    <Box aria-busy={busy} sx={{ p: 2.5, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
      {/* "All" is the unfiltered default, not an explicit selection — the mock (which never
          highlights any filter button) renders it as a plain neutral button. Only the two explicit
          filters (Unmapped/Conflict) get the primary highlight when chosen. */}
      {filterButton("All", undefined, false)}
      {filterButton("Unmapped only", "unmapped", params.mappingState === "unmapped")}
      {filterButton("Conflicts only", "conflict", params.mappingState === "conflict")}
      <Box sx={{ flex: 1 }} />
      <Typography variant="caption" sx={{ color: "text.secondary" }}>
        {selectionVisible ? (
          <>
            Selected <Box component="span" sx={HS_MONO}>{selected?.tag_id}</Box>
          </>
        ) : (
          "Select a tag to act on it"
        )}
      </Typography>
      {/* "Map to animal" is the PRIMARY verb here: on a farm where nothing is mapped yet it is the
          only action that makes this screen useful at all. */}
      {actionButton("map", "Map to animal", true)}
      {actionButton("replace", "Replace tag", false)}
      {actionButton("unmap", "Unmap", false)}
    </Box>
  );

  const body = () => {
    if (items.length === 0) {
      return (
        <EmptyState
          title={params.mappingState ? "No tags in this mapping state" : "No BLE tags to map yet"}
          description={params.mappingState
            ? "Every tag in scope is in a different mapping state — an outcome, not a read failure."
            : "No BLE gateway has posted a tag for this tenant yet. Tags appear here the moment a gateway forwards one, mapped or not."}
          action={params.mappingState ? (
            <Button component={Link} href={herdSignalsHref(params, { hs_map: undefined })} variant="outlined" color="inherit" size="small">
              Show all tags
            </Button>
          ) : undefined}
        />
      );
    }

    return (
      <>
        {pager("top")}
        {/* A write's router.refresh() (no URL change, so no UrlSuspense swap) or a filter / pager
            transition: the rows being replaced give way to the table skeleton, never stay on screen
            clickable (REVIEW-43; no dimming, guard: pending-dim). */}
        {busy ? (
          <TableSkeleton bare header={false} pager={false} columns={10} rows={Math.min(Math.max(items.length, 1), 10)} />
        ) : (
        <>
        {/* Wide table scrolls inside the template Scrollbar (TR1-#20), never outside its card. */}
        <Scrollbar>
          <Table data-testid="herd-signals-mapping-table" sx={respTableSx()}>
            <TableHead>
              <TableRow>
                <TableCell component="th">Animal</TableCell>
                <TableCell component="th">Existing tag 1</TableCell>
                <TableCell component="th">Existing tag 2</TableCell>
                <TableCell component="th">Smart tag capable</TableCell>
                <TableCell component="th">BLE tag ID</TableCell>
                <TableCell component="th">BLE MAC</TableCell>
                <TableCell component="th">Source</TableCell>
                <TableCell component="th">Verified by</TableCell>
                <TableCell component="th">Verified at</TableCell>
                <TableCell component="th">Status</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {items.map((item) => {
                const isSelected = item.tag_id === selectedTagId;
                return (
                  <TableRow
                    key={item.tag_id}
                    sx={selectableRowSx(isSelected)}
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
                    <TableCell data-l="Animal" data-wide>
                      <AnimalCell item={item} />
                    </TableCell>
                    <TableCell data-l="Existing tag 1" sx={item.animal_identifier_1 ? undefined : HS_FAINT}>
                      {item.animal_identifier_1 ? <Box component="span" sx={HS_MONO}>{item.animal_identifier_1}</Box> : NOT_ON_CONTRACT}
                    </TableCell>
                    <TableCell data-l="Existing tag 2" sx={item.animal_identifier_2 ? undefined : HS_FAINT}>
                      {item.animal_identifier_2 ? <Box component="span" sx={HS_MONO}>{item.animal_identifier_2}</Box> : NOT_ON_CONTRACT}
                    </TableCell>
                    {/* Every row on this screen IS a BLE smart tag -- that is why it is here at all.
                        smart_tag_capable is a flag on the ANIMAL IDENTIFIER, so an unmapped tag has
                        no identifier for the flag to live on and the honest value is "not
                        applicable", not "No". Rendering "No" asserted that a real smart tag is not a
                        smart tag. */}
                    <TableCell data-l="Smart tag capable">
                      {item.mapping_state === "mapped" ? (
                        <Tag tone="ok">Yes</Tag>
                      ) : (
                        <Box component="span" sx={HS_FAINT} title="No animal identifier yet, so there is no identifier to carry the flag">
                          {NOT_ON_CONTRACT}
                        </Box>
                      )}
                    </TableCell>
                    <TableCell data-l="BLE tag ID">
                      <Box component="span" sx={HS_MONO}>{item.tag_id}</Box>
                    </TableCell>
                    <TableCell data-l="BLE MAC">
                      <Box component="span" sx={{ ...HS_MONO, ...HS_FAINT }}>{fmtBleMac(item.tag_mac)}</Box>
                    </TableCell>
                    {/* The only provenance the live contract carries: which gateway forwarded the
                        tag. A tag with no gateway id was not attributed to one, so it gets "—",
                        never a guessed source. */}
                    <TableCell data-l="Source">{item.gateway_id ? "Gateway" : <Box component="span" sx={HS_FAINT}>{NOT_ON_CONTRACT}</Box>}</TableCell>
                    {/* The backend names a system binding "System setup" (herdsignals DisplayMappedBy) and
                        passes a person through. TODO: resolve a person's user id to their roster name.
                        The value wraps anywhere: an unbroken id used to paint over the Bound-at cell on
                        a phone. */}
                    <TableCell data-l="Bound by" sx={item.mapped_by ? undefined : HS_FAINT}>
                      {item.mapped_by ? (
                        <Box component="span" sx={{ typography: "caption", overflowWrap: "anywhere", wordBreak: "break-word" }} title={item.mapped_by}>
                          {item.mapped_by}
                        </Box>
                      ) : (
                        NOT_ON_CONTRACT
                      )}
                    </TableCell>
                    <TableCell data-l="Bound at" sx={item.mapped_at ? undefined : HS_FAINT}>
                      {item.mapped_at ? fmtAgo(item.mapped_at, new Date().getTime()) : NOT_ON_CONTRACT}
                    </TableCell>
                    <TableCell data-l="Status">
                      <Tag tone={MAPPING_TONE[item.mapping_state]}>{MAPPING_LABEL[item.mapping_state]}</Tag>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Scrollbar>
        </>
        )}
        {pager("bottom")}
      </>
    );
  };

  return (
    <Stack spacing={2}>
      {flash ? (
        <Alert severity="success" role="status" onClose={() => setFlash(null)}>
          {flash}
        </Alert>
      ) : null}
      <Card>
        <CardHeader
          title="BLE tag ↔ animal identifier mapping"
          subheader="Flag lives on the identifier, not the animal — an animal can carry several tags"
        />
        {toolbar}
        {body()}
      </Card>
      {dialog && selected ? (
        <MappingDialog action={dialog} item={selected} onClose={() => setDialog(null)} onDone={onDone} />
      ) : null}
    </Stack>
  );
}
