"use client";

import { useSyncExternalStore } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { isConfiguredZeroQuantity } from "./feed-quantity-state";
import { rationRateKey, sameRate } from "./feed-rate-optimistic-state";

export { rationRateKey } from "./feed-rate-optimistic-state";

// The authored rate a row shows RIGHT AFTER it was saved, before the server's re-render lands.
//
// THE DEFECT THIS FIXES (maintainer report 2026-08-11): saving a rate writes in ~0.3s, but the new
// number only appears once `revalidatePath("/feed/config")` has re-rendered the whole route — nine
// reads and four tables — which is seconds. In between, the author is looking at a form that closed
// on success while the cell beside it still shows the OLD quantity. On a screen whose numbers are
// feeding instructions, a stale number that looks settled is worse than a slow one that looks busy.
//
// A MODULE-LEVEL STORE, NOT CONTEXT, because the two halves cannot share a React tree here: the
// quantity cell and the Edit control sit in different <td>s of a SERVER-rendered row, with other
// cells between them, so there is no client component that could wrap both without restructuring the
// table. They are both client components in one browser runtime, which is all this needs.
//
// KEYED ON (park, group, tag, item), NEVER ON ration_rate_id. Saving a rate is effective-dated: the
// backend CLOSES the in-force row and OPENS a new one, so the row's id CHANGES across the very write
// this is tracking. The natural key is the only thing stable across it.

type SavedRate = { value: string; at: number };

const saved = new Map<string, SavedRate>();
const listeners = new Set<() => void>();

// How long an unconfirmed value may be shown. Reached only when the server's re-render never brings
// back the value that was written — a failed revalidation, or another author changing the same cell
// in between. Expiring rather than persisting means the screen always converges on the SERVER's
// answer, which is the only authority on what will be fed.
const MAX_AGE_MS = 20_000;

function emit() {
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Records the value a CONFIRMED save wrote.
 *
 * Called at SUBMIT, not after the action resolves, and that ordering is the whole point. A Next
 * server action that calls revalidatePath returns the RE-RENDERED payload in the same response, so
 * anything published after the await lands no earlier than the render it was meant to pre-empt —
 * which is exactly what the first version of this got wrong and a browser edit measured at 9.3s.
 *
 * The write is not confirmed yet at this moment, so a REFUSED one must be rolled back by the caller
 * (clearSavedRate) rather than left on screen.
 */
/** Drops a value published at submit time whose write was then REFUSED. */
export function clearSavedRate(key: string): void {
  if (saved.delete(key)) emit();
}

export function publishSavedRate(key: string, value: string): void {
  const trimmed = value.trim();
  if (trimmed === "") return;
  const entry: SavedRate = { value: trimmed, at: Date.now() };
  saved.set(key, entry);
  emit();
  // An ACTIVE expiry, not one evaluated on the next render. Nothing else is guaranteed to re-render
  // this row — if the server's answer never arrives, no store emit and no parent render happen — so
  // a passive check would leave an unconfirmed number on screen indefinitely. The timer is the only
  // thing that makes the 20s ceiling real.
  setTimeout(() => {
    if (saved.get(key) === entry) {
      saved.delete(key);
      emit();
    }
  }, MAX_AGE_MS);
}

/**
 * What the quantity cell should render: the just-saved value while the server is still catching up,
 * and the server's own value the moment it agrees or the entry ages out.
 *
 * Dropping the entry as soon as the server matches is what keeps this from becoming a second source
 * of truth: once the re-render lands, this store has nothing to say about the row.
 */
export function useDisplayedRate(key: string, serverValue: string): { value: string; pending: boolean } {
  const entry = useSyncExternalStore(
    subscribe,
    () => saved.get(key),
    () => undefined, // server render: always the server's own value
  );
  if (!entry) return { value: serverValue, pending: false };
  if (sameRate(entry.value, serverValue)) {
    // Landed (or gave up). Clear on the next tick rather than during render, so this render stays
    // pure and every subscribed row is told once.
    queueMicrotask(() => {
      const current = saved.get(key);
      if (current && current.at === entry.at) {
        saved.delete(key);
        emit();
      }
    });
    return { value: serverValue, pending: false };
  }
  return { value: entry.value, pending: true };
}

/**
 * The ration grid's quantity cell.
 *
 * A client component ONLY so it can show a just-saved value before the route's re-render lands; the
 * number itself still comes from the server on every render this store has nothing pending for.
 *
 * The authored-zero tag travels with it, because that distinction survives the optimistic path too:
 * an authored 0 means "feed nothing of this item" and is correct for milk-fed kids, while an absent
 * rate means UNCONFIGURED and blocks the shed. Rendering a freshly-saved 0 without its tag would put
 * the more dangerous reading on screen for exactly as long as the stale-number problem this fixes.
 */
export function RationRateValue({
  pageContract,
  parkId,
  rationGroup,
  shedTag,
  feedItem,
  gramsPerHead,
}: {
  pageContract: AdminUiPageContract;
  parkId: string;
  rationGroup: string;
  shedTag: string;
  feedItem: string;
  gramsPerHead: string;
}) {
  const key = rationRateKey(parkId, rationGroup, shedTag, feedItem);
  const { value, pending } = useDisplayedRate(key, gramsPerHead);
  const authoredZero = isConfiguredZeroQuantity(value);
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>
      <span
        style={{
          fontVariantNumeric: "tabular-nums",
          fontWeight: authoredZero ? 500 : 700,
          color: authoredZero ? "var(--muted)" : "var(--brand-d)",
        }}
      >
        {value}
      </span>
      {authoredZero ? (
        <span className="tag t-info" title={copy(pageContract, "label.configured_zero_note")}>
          {copy(pageContract, "label.configured_zero")}
        </span>
      ) : null}
      {/* Said out loud while the server catches up, rather than shown as a settled number. The write
          IS committed at this point -- the form only closes on a confirmed save -- so this reports
          that the rest of the page has not caught up yet, not that the value is in doubt. */}
      {pending ? (
        <span className="muted" style={{ display: "inline-flex", alignItems: "center", gap: 5, fontSize: 11 }} role="status">
          <span className="wfspin" aria-hidden="true" style={{ width: 11, height: 11 }} />
          {copy(pageContract, "state.loading")}
        </span>
      ) : null}
    </span>
  );
}
