"use client";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

// "Tag animals to sale": pick the real animals a recorded sale is made of.
//
// THE FLOW, and why it has three steps rather than one:
//
//   pick    filter to a park, shed and pens; multi-select across as many sheds as the sale spans,
//           with a running count on the right
//   review  "Done" asks the backend what was picked, grouped shed-wise, and what it refuses
//   confirm the animals are tagged to the sale and marked sold
//
// The review step exists because selling is irreversible in the world — the animal physically
// leaves — so a person is owed one chance to read the list back before it happens.
//
// NO VERDICT IS COMPUTED HERE. `sellable`, `blocker` and `blocked_reason` all arrive from the
// backend and are rendered verbatim. The component never decides an animal is fine to sell.

import { PackageCheck } from "lucide-react";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import Paper from "@mui/material/Paper";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import FormHelperText from "@mui/material/FormHelperText";
import { Caption } from "@/components/app/caption";
import { DetailDrawer, DrawerTableScroll } from "@/components/app/detail-drawer";
import { useCallback, useEffect, useState, useSyncExternalStore, useTransition } from "react";

import { LOCAL_OVERLAY_URL_CHANGE_EVENT, replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import type { SaleAllocationPreviewResponse, SaleCandidate } from "@/lib/api/server";
import type { SalesDeal } from "@/lib/api/procurement";
import {
  confirmSaleAllocationAction,
  fetchSaleCandidatesAction,
  previewSaleAllocationAction,
} from "./sale-allocation-actions";

import type { SaleLocationCatalog } from "@/lib/api/server";
import { FormSelect } from "./form-select";
import { listOptions } from "./option-utils";
import Checkbox from "@mui/material/Checkbox";

const TAG_PARAM = "tag_sale";

function readTagParam(): string {
  return new URL(window.location.href).searchParams.get(TAG_PARAM) ?? "";
}

function subscribeToOverlayUrl(onChange: () => void): () => void {
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  window.addEventListener("popstate", onChange);
  return () => {
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
    window.removeEventListener("popstate", onChange);
  };
}

type Step = "pick" | "review" | "done";

export function SaleAllocationDrawer({
  deals,
  locations,
  pageContract,
  listHref,
}: {
  /** The rendered ledger page; the drawer names the sale from it and issues no fetch for it. */
  deals: SalesDeal[];
  /**
   * null means the catalog could NOT be read -- a different fact from a farm with no sheds,
   * and the two are given different copy. A silent empty here reads as a broken picker.
   */
  locations: SaleLocationCatalog | null;
  pageContract: AdminUiPageContract;
  listHref: string;
}) {
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readTagParam, () => "");
  const open = selection !== "";

  // WHICH SALE is client state, not the URL param. The param only says the drawer is
  // open and which sale it opened on; a person tagging animals routinely needs a
  // different sale from whichever one their click happened to start from, and making
  // them close, find the row and reopen is the bug this select fixes.
  const [dealId, setDealId] = useState("");
  const deal = deals.find((d) => d.deal_id === (dealId || selection)) ?? null;

  const [parkId, setParkId] = useState("");
  // ONE operational-location filter, pen-wise: "Castro 1", not "Castro" plus a pen
  // chooser. The key is shed id + pen because two parks own a shed called "Castro" and a
  // label alone would merge them.
  const [locationKey, setLocationKey] = useState("");
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<SaleCandidate[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  // Selection is keyed by goat id and SURVIVES a filter change: a sale routinely spans several
  // sheds, so moving the filter to the next shed must not silently discard what is already picked.
  const [picked, setPicked] = useState<Map<string, SaleCandidate>>(new Map());
  const [step, setStep] = useState<Step>("pick");
  const [preview, setPreview] = useState<SaleAllocationPreviewResponse | null>(null);
  // Live weight per picked goat id, as typed (maintainer decision 2026-09-08: required for
  // every animal). Kept as strings so the backend validates the number; nothing is parsed here.
  const [weights, setWeights] = useState<Map<string, string>>(new Map());
  const [weightAttempted, setWeightAttempted] = useState(false);
  const [confirmed, setConfirmed] = useState<{ allocated: number } | null>(null);
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();

  const close = useCallback(() => {
    replaceLocalOverlayUrl(listHref);
  }, [listHref]);

  // Reset every time a different sale is opened, so one sale's picks can never be confirmed
  // against another.
  /* eslint-disable react-hooks/set-state-in-effect -- URL-controlled drawer state must be reset synchronously when the selected sale changes. */
  useEffect(() => {
    if (!open) return;
    setDealId(selection);
    setParkId("");
    setLocationKey("");
    setQuery("");
    setCandidates([]);
    setCursor(null);
    setPicked(new Map());
    setWeights(new Map());
    setWeightAttempted(false);
    setStep("pick");
    setPreview(null);
    setConfirmed(null);
    setError("");
  }, [open, selection]);
  /* eslint-enable react-hooks/set-state-in-effect */

  // Escape and the backdrop reach `close` through the template Drawer's onClose; MUI traps focus in
  // the paper and returns it to the row that opened it.

  const loadCandidates = useCallback(
    (nextCursor?: string) => {
      if (!parkId) return;
      const chosen = (locations?.locations ?? []).find((l) => locationEntryKey(l) === locationKey);
      setError("");
      startTransition(async () => {
        const result = await fetchSaleCandidatesAction({
          parkId,
          shedId: chosen?.shed_id || undefined,
          partitionLabels: chosen?.partition_label ? [chosen.partition_label] : [],
          query: query || undefined,
          cursor: nextCursor,
        });
        if (!result.ok) {
          setError(result.message);
          return;
        }
        // Appending on a cursor, replacing on a fresh filter: the "load more" path must not
        // discard the rows already on screen.
        setCandidates((current) =>
          nextCursor ? [...current, ...result.data.candidates] : result.data.candidates,
        );
        setCursor(result.data.next_cursor ?? null);
      });
    },
    [parkId, locationKey, locations, query],
  );

  /* eslint-disable react-hooks/set-state-in-effect -- filter changes must immediately replace the picker page; the async action owns the resulting state. */
  useEffect(() => {
    if (!open || !parkId) return;
    loadCandidates();
    // loadCandidates already closes over every filter, so this reloads on any filter change.
  }, [open, parkId, locationKey, query, loadCandidates]);
  /* eslint-enable react-hooks/set-state-in-effect */

  if (!open || !deal) return null;

  const locationsInPark = (locations?.locations ?? []).filter((l) => !parkId || l.park_id === parkId);
  const pickedList = [...picked.values()];
  // How many animals this sale is FOR. Read from the ledger row already on the page, so
  // the count is visible while picking rather than only after Done. The server enforces
  // the same number on confirm -- this is the operator's guide, never the gate.
  const target = Math.floor(deal.animal_count ?? 0);
  const remaining = Math.max(0, target - pickedList.length);
  const overPicked = target > 0 && pickedList.length > target;
  // Exactly-filled is the only state that may proceed, matching the server rule: a sale
  // cannot be tagged half now and half later.
  const canReview = target > 0 && pickedList.length === target;

  const toggle = (candidate: SaleCandidate) => {
    if (!candidate.sellable) return;
    setPicked((current) => {
      const next = new Map(current);
      if (next.has(candidate.goat_id)) {
        next.delete(candidate.goat_id);
        return next;
      }
      // Stop AT the target rather than letting the count run past it and refusing later:
      // an operator who has ticked 6 for a 5-animal sale has to work out which one to
      // untick, and the screen never said which was the extra.
      if (target > 0 && next.size >= target) return current;
      next.set(candidate.goat_id, candidate);
      return next;
    });
  };

  const goToReview = () => {
    setError("");
    startTransition(async () => {
      const result = await previewSaleAllocationAction({
        salesDealId: deal.deal_id,
        goatIds: pickedList.map((c) => c.goat_id),
      });
      if (!result.ok) {
        setError(result.message);
        return;
      }
      setPreview(result.data);
      setStep("review");
    });
  };

  // The animals the review CLEARED: the ones a weight is owed for and the ones confirm sends.
  const clearedAnimals = pickedList.filter(
    (c) => !(preview?.blocked_animals ?? []).some((b) => b.goat_id === c.goat_id),
  );
  const weightOf = (goatId: string) => (weights.get(goatId) ?? "").trim();
  // A weight must be present and look like kg (more than zero, up to two decimals) for EVERY
  // cleared animal before confirm is offered. The backend re-checks; this only stops a click
  // that the server would refuse.
  const weightLooksValid = (raw: string) => /^\d{1,5}(\.\d{1,2})?$/.test(raw) && Number(raw) > 0;
  const allWeighed = clearedAnimals.length > 0 && clearedAnimals.every((c) => weightLooksValid(weightOf(c.goat_id)));

  const confirm = () => {
    setError("");
    setWeightAttempted(true);
    if (!allWeighed) return;
    startTransition(async () => {
      // Only the animals the review CLEARED are sent. A refused one would fail the whole
      // confirmation, which is the backend's fail-closed rule, and there is no way past it here.
      const clearedIds = clearedAnimals.map((c) => c.goat_id);
      const animalWeightsKg: Record<string, string> = {};
      for (const id of clearedIds) animalWeightsKg[id] = weightOf(id);
      const result = await confirmSaleAllocationAction({
        salesDealId: deal.deal_id,
        goatIds: clearedIds,
        animalWeightsKg,
      });
      if (!result.ok) {
        setError(result.message);
        return;
      }
      setConfirmed({ allocated: result.data.allocated });
      setStep("done");
    });
  };

  const title = copy(pageContract, "action.tag_animals.label");
  const pickedByLocation = [...new Set(pickedList.map((c) => c.operational_location_display))];

  return (
    <DetailDrawer
      open={open}
      onClose={close}
      title={title}
      icon={<PackageCheck aria-hidden />}
      ariaLabel={title}
      closeLabel={copy(pageContract, "action.close")}
      paperTestId="sale-allocation-drawer"
      footer={
        <>
          {step === "pick" ? (
            <>
              {/* The sale count is the target, and a partial mapping cannot proceed:
                  a half-tagged sale leaves the ledger saying one thing and the herd
                  another, with no screen showing the gap. */}
              {!canReview ? (
                <Typography variant="body2" sx={{ color: "text.secondary", flex: "1 1 auto", alignSelf: "center", minWidth: 0 }}>
                  {target === 0
                    ? copy(pageContract, "hint.sale_no_count")
                    : overPicked
                      ? copy(pageContract, "hint.too_many")
                      : `${copy(pageContract, "hint.pick_all_prefix")} ${target} ${copy(pageContract, "hint.pick_all_suffix")}`}
                </Typography>
              ) : null}
              <Button variant="contained" color="primary" disabled={!canReview || pending} onClick={goToReview}>
                {copy(pageContract, "action.done")}
              </Button>
            </>
          ) : null}
          {step === "review" ? (
            <>
              <Button variant="outlined" color="inherit" onClick={() => setStep("pick")} disabled={pending}>
                {copy(pageContract, "action.back")}
              </Button>
              <Button variant="contained" color="primary" onClick={confirm} disabled={pending || !preview?.complete}>
                {copy(pageContract, "action.confirm_sold")}
              </Button>
            </>
          ) : null}
          {step === "done" ? (
            <Button variant="contained" color="primary" onClick={close}>
              {copy(pageContract, "action.close")}
            </Button>
          ) : null}
        </>
      }
    >
      {/* WHICH SALE these animals are being tagged to. Editable while picking, locked once the
          review step is reached: changing the sale under a reviewed list would silently re-point
          animals a person already checked. */}
      <FormSelect
        label={copy(pageContract, "field.sale")}
        value={deal?.deal_id ?? ""}
        onValueChange={setDealId}
        disabled={step !== "pick"}
        options={listOptions(deals, (d) => d.deal_id, (d) => dealOptionLabel(d))}
      />
      {error ? <Alert severity="error">{error}</Alert> : null}

      {step === "pick" ? (
        <>
          {/* The running count, above the list (the drawer is one column): how many are picked
              right now, and from where -- so a person can see the sale taking shape before they
              commit. */}
          <Paper variant="outlined" sx={{ p: 2, display: "flex", flexDirection: "column", gap: 1 }} data-testid="sale-tag-count">
            <Stack direction="row" spacing={1} sx={{ alignItems: "baseline", flexWrap: "wrap" }}>
              <Typography variant="h4" component="b">
                {pickedList.length}
                {target > 0 ? (
                  <Typography component="span" variant="h6" sx={{ color: "text.secondary" }}>
                    {" "}/ {target}
                  </Typography>
                ) : null}
              </Typography>
              <Typography variant="body2" sx={{ color: "text.secondary" }}>{copy(pageContract, "label.selected")}</Typography>
              {target > 0 && remaining > 0 ? (
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  · {remaining} {copy(pageContract, "label.still_to_pick")}
                </Typography>
              ) : null}
              {canReview ? <Tag tone="ok">{copy(pageContract, "label.all_picked")}</Tag> : null}
            </Stack>
            {pickedByLocation.length > 0 ? (
              <Stack direction="row" sx={{ flexWrap: "wrap", gap: 1 }}>
                {pickedByLocation.map((label) => (
                  <Tag key={label} tone="mut">
                    {label} · {pickedList.filter((c) => c.operational_location_display === label).length}
                  </Tag>
                ))}
              </Stack>
            ) : null}
          </Paper>

          <Stack spacing={2}>
            <div>
              <FormSelect
                label={copy(pageContract, "field.park")}
                id="tag-park"
                value={parkId}
                onValueChange={(next) => { setParkId(next); setLocationKey(""); }}
                options={listOptions(
                  locations?.parks ?? [],
                  (p) => p.park_id,
                  (p) => p.label,
                  copy(pageContract, "value.choose_park"),
                )}
              />
              {/*
                A picker with nothing in it must SAY why. The two reasons are different facts and
                carry different backend copy: the catalog read failed (usually a missing grant,
                which a person can get fixed), or the farm genuinely has no shed to sell out of.
              */}
              {locations === null ? (
                <FormHelperText error>{copy(pageContract, "empty.parks_unavailable")}</FormHelperText>
              ) : locations.parks.length === 0 ? (
                <FormHelperText>{copy(pageContract, "empty.parks")}</FormHelperText>
              ) : null}
            </div>
            <FormSelect
              label={copy(pageContract, "field.shed")}
              id="tag-loc"
              value={locationKey}
              onValueChange={setLocationKey}
              disabled={!parkId}
              options={listOptions(
                locationsInPark,
                (l) => locationEntryKey(l),
                (l) => l.operational_location_display,
                copy(pageContract, "value.all_sheds"),
              )}
            />
            <TextField
              id="tag-q"
              fullWidth
              label={copy(pageContract, "field.search_tag")}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={copy(pageContract, "value.search_tag_hint")}
              slotProps={{ htmlInput: { maxLength: 80 }, inputLabel: { shrink: true } }}
            />
          </Stack>

          <DrawerTableScroll>
            <Table size="small" sx={{ minWidth: 400 }} aria-label={title}>
              <TableBody>
                {candidates.map((c) => {
                  const on = picked.has(c.goat_id);
                  return (
                    <TableRow key={c.goat_id} sx={c.sellable ? undefined : { "& td": { color: "text.disabled" } }}>
                      <TableCell padding="checkbox">
                        <Checkbox
                          checked={on}
                          disabled={!c.sellable}
                          onChange={() => toggle(c)}
                          sx={{ p: { xs: 1.5, sm: 1 } }}
                          slotProps={{ input: { "aria-label": animalLabel(c) } }}
                        />
                      </TableCell>
                      {/* BOTH tags, because the search matches either one. A row showing only the
                          primary answered a search for the secondary with a number that reads as a
                          different animal. */}
                      <TableCell>
                        <Typography variant="subtitle2" component="b">{c.tag_number || c.display_id}</Typography>
                        {c.secondary_tag_number ? (
                          <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{c.secondary_tag_number}</Typography>
                        ) : null}
                      </TableCell>
                      <TableCell>{c.operational_location_display}</TableCell>
                      <TableCell>
                        {/* The refusal is the backend's sentence, verbatim. */}
                        {c.sellable ? null : <Tag tone="warn">{c.blocked_reason}</Tag>}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </DrawerTableScroll>
          {cursor ? (
            <Button variant="outlined" color="inherit" onClick={() => loadCandidates(cursor)} disabled={pending} sx={{ alignSelf: "flex-start" }}>
              {copy(pageContract, "action.load_more")}
            </Button>
          ) : null}
        </>
      ) : null}

      {step === "review" && preview ? (
        <>
          <Caption>{copy(pageContract, "hint.review")}</Caption>
          {preview.shed_groups.map((group) => (
            <Paper key={`${group.shed_id}|${group.partition_label ?? ""}`} variant="outlined" sx={{ p: 2, display: "flex", flexDirection: "column", gap: 1 }}>
              <Typography variant="subtitle2">
                {group.operational_location_display}{" "}
                <Typography component="span" variant="body2" sx={{ color: "text.secondary" }}>· {group.animals}</Typography>
              </Typography>
              <Stack direction="row" sx={{ flexWrap: "wrap", gap: 1 }}>
                {group.tag_numbers.map((tag) => (
                  <Tag key={tag} tone="mut">{tag}</Tag>
                ))}
              </Stack>
            </Paper>
          ))}
          {/* Weight at tagging (maintainer decision 2026-09-08): one box per cleared animal, every
              one required. The value goes to the backend as typed. */}
          <Paper variant="outlined" sx={{ p: 2, display: "flex", flexDirection: "column", gap: 1 }}>
            <Typography variant="subtitle2">{copy(pageContract, "field.animal_weight")}</Typography>
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "hint.animal_weight")}
            </Typography>
            <DrawerTableScroll>
              <Table size="small" sx={{ minWidth: 360 }} aria-label={copy(pageContract, "field.animal_weight")}>
                <TableBody>
                  {clearedAnimals.map((c) => {
                    const raw = weightOf(c.goat_id);
                    const bad = weightAttempted ? !weightLooksValid(raw) : raw !== "" && !weightLooksValid(raw);
                    return (
                      <TableRow key={c.goat_id}>
                        <TableCell>
                          <Typography variant="subtitle2" component="b">{c.tag_number || c.display_id}</Typography>
                          {c.secondary_tag_number ? (
                            <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{c.secondary_tag_number}</Typography>
                          ) : null}
                        </TableCell>
                        <TableCell>{c.operational_location_display}</TableCell>
                        <TableCell>
                          <TextField
                            size="small"
                            required
                            error={bad}
                            value={weights.get(c.goat_id) ?? ""}
                            onChange={(e) => {
                              const next = new Map(weights);
                              next.set(c.goat_id, e.target.value);
                              setWeights(next);
                              if (weightAttempted && clearedAnimals.every((animal) => weightLooksValid((next.get(animal.goat_id) ?? "").trim()))) {
                                setWeightAttempted(false);
                              }
                            }}
                            sx={{ width: 1 }}
                            slotProps={{
                              htmlInput: {
                                inputMode: "decimal",
                                "aria-label": `${copy(pageContract, "field.animal_weight")} ${animalLabel(c)}`,
                                "aria-invalid": bad,
                              },
                            }}
                          />
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </DrawerTableScroll>
          </Paper>
          {preview.blocked_animals.length > 0 ? (
            <Paper variant="outlined" sx={{ p: 2, display: "flex", flexDirection: "column", gap: 1 }}>
              <Typography variant="subtitle2">{copy(pageContract, "label.cannot_sell")}</Typography>
              <Stack component="ul" spacing={0.5} sx={{ m: 0, pl: 2.5, typography: "body2" }}>
                {preview.blocked_animals.map((c) => (
                  <li key={c.goat_id}>
                    {animalLabel(c)} — {c.blocked_reason}
                  </li>
                ))}
              </Stack>
            </Paper>
          ) : null}
        </>
      ) : null}

      {step === "done" && confirmed ? (
        <Alert severity="success">
          {confirmed.allocated} {copy(pageContract, "label.marked_sold")}
        </Alert>
      ) : null}
    </DetailDrawer>
  );
}

/**
 * One line identifying a sale in the selector: date, buyer, place, and what was sold.
 *
 * The counts matter more than they look — a buyer often has several deals, and the head
 * count plus product type is what tells a person which of them they are holding animals
 * for. Composed from the ledger row already on the page; no extra read.
 */
function dealOptionLabel(deal: SalesDeal): string {
  const who = [deal.buyer_name, deal.buyer_place].filter(Boolean).join(" · ");
  const what = [
    deal.animal_count ? `${deal.animal_count} ${deal.product_type}` : deal.product_type,
    deal.breed,
  ]
    .filter(Boolean)
    .join(" · ");
  // DD/MM/YYYY like every visible date (2026-09-10 lock); the ISO sale_date is wire format.
  return [deal.sale_date ? fmtDate(deal.sale_date) : "", who, what].filter(Boolean).join("  —  ");
}

/**
 * One animal named the way the person reading it off the ear does: both tags when it
 * carries two, because the picker's search matches either of them.
 */
function animalLabel(c: SaleCandidate): string {
  const tags = [c.tag_number, c.secondary_tag_number].filter(Boolean).join(" · ");
  return tags || c.display_id || "";
}

/**
 * Stable key for one operational location.
 *
 * Shed id plus pen, never the label: two parks genuinely own a shed called "Castro", so a
 * label-keyed option list would merge them into one entry and silently filter the wrong
 * park's animals.
 */
function locationEntryKey(entry: { shed_id: string; partition_label?: string }): string {
  return `${entry.shed_id}|${entry.partition_label ?? ""}`;
}
