/**
 * Route aliases resolved in proxy.ts BEFORE any page renders, so an alias never paints a skeleton
 * and then redirects. The contract on this stack serves
 * Query strings (scope params) ride along unchanged.
 */
export const ROUTE_ALIASES: Readonly<Record<string, string>> = {
  // Actions was renamed Verify (maintainer decision 2026-08-12); see app/(admin)/actions/page.tsx.
  "/actions": "/verify",
  "/verification": "/verify",
  // Two vertical HOMES that are redirects and nothing else. Left to their own `redirect()` they
  // stream under the shell skeleton, blank, and then skeleton again on the target -- the route
  // lane sees two skeleton phases on one navigation. Resolved here they cost one document and
  // one skeleton, like every other alias. The proxy keeps the query string, which is exactly what
  // /sales used to re-forward by hand.
  "/procurement": "/procurement/source-entry",
  "/sales": "/sales/sold",
};

/**
 * Aliases that apply only when the request carries NONE of the listed params: the bare editor
 * URL has nothing to edit and the page itself redirects to the console (after painting a skeleton).
 */
export const ROUTE_ALIASES_WITHOUT_PARAMS: ReadonlyArray<{ from: string; to: string; unlessParams: readonly string[] }> = [
  { from: "/vaccination/plan/edit", to: "/vaccination/plan", unlessParams: ["version"] },
];
