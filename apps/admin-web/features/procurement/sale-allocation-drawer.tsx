"use client";

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

import { PackageCheck, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore, useTransition } from "react";

import { LOCAL_OVERLAY_URL_CHANGE_EVENT, replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SaleAllocationPreviewResponse, SaleCandidate } from "@/lib/api/server";
import type { SalesDeal } from "@/lib/api/procurement";
import {
  confirmSaleAllocationAction,
  fetchSaleCandidatesAction,
  previewSaleAllocationAction,
} from "./sale-allocation-actions";

import type { SaleLocationCatalog } from "@/lib/api/server";

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
  locations: SaleLocationCatalog;
  pageContract: AdminUiPageContract;
  listHref: string;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
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
    setStep("pick");
    setPreview(null);
    setConfirmed(null);
    setError("");
    closeButtonRef.current?.focus();
  }, [open, selection]);
  /* eslint-enable react-hooks/set-state-in-effect */

  useEffect(() => {
    if (!open) return undefined;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, close]);

  const loadCandidates = useCallback(
    (nextCursor?: string) => {
      if (!parkId) return;
      const chosen = locations.locations.find((l) => locationEntryKey(l) === locationKey);
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

  const locationsInPark = locations.locations.filter((l) => !parkId || l.park_id === parkId);
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

  const confirm = () => {
    setError("");
    startTransition(async () => {
      // Only the animals the review CLEARED are sent. A refused one would fail the whole
      // confirmation, which is the backend's fail-closed rule, and there is no way past it here.
      const clearedIds = pickedList
        .map((c) => c.goat_id)
        .filter((id) => !(preview?.blocked_animals ?? []).some((b) => b.goat_id === id));
      const result = await confirmSaleAllocationAction({
        salesDealId: deal.deal_id,
        goatIds: clearedIds,
      });
      if (!result.ok) {
        setError(result.message);
        return;
      }
      setConfirmed({ allocated: result.data.allocated });
      setStep("done");
    });
  };

  return (
    <>
      <div className="scrim on" onClick={close} aria-hidden />
      <aside className="drawer on sales-tagdrawer" role="dialog" aria-modal="true" aria-label={copy(pageContract, "action.tag_animals.label")}>
        <div className="dh">
          <b>
            <PackageCheck className="ic" size={15} aria-hidden /> {copy(pageContract, "action.tag_animals.label")}
          </b>
          <button ref={closeButtonRef} type="button" className="iconbtn" onClick={close} aria-label={copy(pageContract, "action.close")}>
            <X size={15} aria-hidden />
          </button>
        </div>

        <div className="dc">
          {/* WHICH SALE these animals are being tagged to. Editable while picking, locked
              once the review step is reached: changing the sale under a reviewed list would
              silently re-point animals a person already checked. */}
          <label className="sales-tagdeal">
            <span className="muted small">{copy(pageContract, "field.sale")}</span>
            <select
              value={deal?.deal_id ?? ""}
              onChange={(e) => setDealId(e.target.value)}
              disabled={step !== "pick"}
            >
              {deals.map((d) => (
                <option key={d.deal_id} value={d.deal_id}>
                  {dealOptionLabel(d)}
                </option>
              ))}
            </select>
          </label>
          {error ? <div className="banner err">{error}</div> : null}

          {step === "pick" ? (
            <div className="sales-tagpick">
              <div className="sales-tagpick-main">
                <div className="sales-tagfilters">
                  <div className="fld">
                    <label htmlFor="tag-park">{copy(pageContract, "field.park")}</label>
                    <select
                      id="tag-park"
                      value={parkId}
                      onChange={(e) => { setParkId(e.target.value); setLocationKey(""); }}
                    >
                      <option value="">{copy(pageContract, "value.choose_park")}</option>
                      {locations.parks.map((p) => (
                        <option key={p.park_id} value={p.park_id}>{p.label}</option>
                      ))}
                    </select>
                  </div>
                  <div className="fld">
                    <label htmlFor="tag-loc">{copy(pageContract, "field.shed")}</label>
                    <select
                      id="tag-loc"
                      value={locationKey}
                      onChange={(e) => setLocationKey(e.target.value)}
                      disabled={!parkId}
                    >
                      <option value="">{copy(pageContract, "value.all_sheds")}</option>
                      {locationsInPark.map((l) => (
                        <option key={locationEntryKey(l)} value={locationEntryKey(l)}>
                          {l.operational_location_display}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="fld">
                    <label htmlFor="tag-q">{copy(pageContract, "field.search_tag")}</label>
                    <input
                      id="tag-q"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                      maxLength={80}
                      placeholder={copy(pageContract, "value.search_tag_hint")}
                    />
                  </div>
                </div>

                <div className="tablewrap">
                  <table className="tbl sales-tagtable">
                    <tbody>
                      {candidates.map((c) => {
                        const on = picked.has(c.goat_id);
                        return (
                          <tr key={c.goat_id} className={c.sellable ? "" : "muted"}>
                            <td>
                              <input
                                type="checkbox"
                                checked={on}
                                disabled={!c.sellable}
                                onChange={() => toggle(c)}
                                aria-label={c.tag_number || c.display_id || ""}
                              />
                            </td>
                            <td><b>{c.tag_number || c.display_id}</b></td>
                            <td>{c.operational_location_display}</td>
                            <td>
                              {/* The refusal is the backend's sentence, verbatim. */}
                              {c.sellable ? null : <Tag tone="warn">{c.blocked_reason}</Tag>}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                {cursor ? (
                  <button type="button" className="btn" onClick={() => loadCandidates(cursor)} disabled={pending}>
                    {copy(pageContract, "action.load_more")}
                  </button>
                ) : null}
              </div>

              {/* The running count, on the right, as asked: how many are picked right now, and
                  from where — so a person can see the sale taking shape before they commit. */}
              <aside className="sales-tagpick-side">
                <div className="sales-tagcount">
                  <b>
                    {pickedList.length}
                    {target > 0 ? <span className="sales-tagtarget"> / {target}</span> : null}
                  </b>
                  <span className="muted small">{copy(pageContract, "label.selected")}</span>
                  {target > 0 && remaining > 0 ? (
                    <span className="muted small">
                      {remaining} {copy(pageContract, "label.still_to_pick")}
                    </span>
                  ) : null}
                  {canReview ? (
                    <Tag tone="ok">{copy(pageContract, "label.all_picked")}</Tag>
                  ) : null}
                </div>
                <ul className="sales-taglist small">
                  {[...new Set(pickedList.map((c) => c.operational_location_display))].map((label) => (
                    <li key={label}>
                      {label} · {pickedList.filter((c) => c.operational_location_display === label).length}
                    </li>
                  ))}
                </ul>
              </aside>
            </div>
          ) : null}

          {step === "review" && preview ? (
            <div className="sales-tagreview">
              <p className="muted small">{copy(pageContract, "hint.review")}</p>
              {preview.shed_groups.map((group) => (
                <section key={`${group.shed_id}|${group.partition_label ?? ""}`} className="card">
                  <b>{group.operational_location_display}</b>{" "}
                  <span className="muted small">· {group.animals}</span>
                  <div className="chiprow">
                    {group.tag_numbers.map((tag) => (
                      <Tag key={tag} tone="mut">{tag}</Tag>
                    ))}
                  </div>
                </section>
              ))}
              {preview.blocked_animals.length > 0 ? (
                <section className="card">
                  <b>{copy(pageContract, "label.cannot_sell")}</b>
                  <ul className="sales-taglist small">
                    {preview.blocked_animals.map((c) => (
                      <li key={c.goat_id}>
                        {c.tag_number || c.display_id} — {c.blocked_reason}
                      </li>
                    ))}
                  </ul>
                </section>
              ) : null}
            </div>
          ) : null}

          {step === "done" && confirmed ? (
            <div className="banner ok">
              {confirmed.allocated} {copy(pageContract, "label.marked_sold")}
            </div>
          ) : null}
        </div>

        <div className="df">
          {step === "pick" ? (
            <>
              {/* The sale's own count is the target, and a partial mapping cannot proceed:
                  a half-tagged sale leaves the ledger saying one thing and the herd
                  another, with no screen showing the gap. */}
              {!canReview ? (
                <span className="muted small">
                  {target === 0
                    ? copy(pageContract, "hint.sale_no_count")
                    : overPicked
                      ? copy(pageContract, "hint.too_many")
                      : `${copy(pageContract, "hint.pick_all_prefix")} ${target} ${copy(pageContract, "hint.pick_all_suffix")}`}
                </span>
              ) : null}
              <button type="button" className="btn primary" disabled={!canReview || pending} onClick={goToReview}>
                {copy(pageContract, "action.done")}
              </button>
            </>
          ) : null}
          {step === "review" ? (
            <>
              <button type="button" className="btn" onClick={() => setStep("pick")} disabled={pending}>
                {copy(pageContract, "action.back")}
              </button>
              <button
                type="button"
                className="btn primary"
                onClick={confirm}
                disabled={pending || !preview?.complete}
              >
                {copy(pageContract, "action.confirm_sold")}
              </button>
            </>
          ) : null}
          {step === "done" ? (
            <button type="button" className="btn primary" onClick={close}>
              {copy(pageContract, "action.close")}
            </button>
          ) : null}
        </div>
      </aside>
    </>
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
  return [deal.sale_date, who, what].filter(Boolean).join("  —  ");
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
