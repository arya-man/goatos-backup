// The one rule deciding what a worklist filter control SHOWS while a URL-driven navigation is still
// in flight. Kept in its own module, free of React and of "use client", so it can be unit-tested as
// the pure decision it is — the bug it fixes was invisible to every source-regex test of the page.
//
// THE BUG THIS EXISTS TO PREVENT (Feed Config, reproduced 2026-08-10): clearing a filter DELETES its
// parameter, and the control then read `params.get(param) ?? serverValue`. With the parameter gone,
// the `??` fell straight through to the STALE server prop — so picking "All" on Shed tag snapped the
// control back to "Pregnant" and held it there for the whole ~0.9s round trip, and "Clear all" left
// every select showing its old value for ~1.3s while the button itself vanished at once. Both read
// to an operator as "the filter is not applying". SETTING a value never showed it, because then the
// parameter is present and the `??` is never reached; only clearing did, which is why the multi-select
// (no `??`) cleared correctly on the same bar while the plain selects did not.

/**
 * The value a filter control must render right now.
 *
 * @param fromUrl        the optimistic URL's value for this parameter, or null when absent.
 * @param serverValue    what the server last rendered for this control.
 * @param optimisticPending true while an optimistic search is live, i.e. the operator has changed a
 *                       filter and the server has not answered yet.
 * @param clearable      whether the operator can clear this control at all. This is the crux: while
 *                       a navigation is pending, an ABSENT parameter on a clearable control means
 *                       "just cleared" and must render empty. On a control that CANNOT be cleared
 *                       (the Park picker, `allowAll: false`) an absent parameter is the normal state
 *                       — the park came from the top bar or the locations fallback — so it keeps the
 *                       server value and does not blank itself when a SIBLING filter changes.
 */
export function worklistFilterShownValue(
  fromUrl: string | null,
  serverValue: string,
  optimisticPending: boolean,
  clearable: boolean,
): string {
  if (fromUrl !== null) return fromUrl;
  return optimisticPending && clearable ? "" : serverValue;
}
