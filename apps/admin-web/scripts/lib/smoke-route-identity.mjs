export function assertSmokeRouteIdentity(expectedUrl, actualUrl) {
  const expected = new URL(expectedUrl), actual = new URL(actualUrl);
  const canonicalRedirect = canonicalSmokeRedirect(expected.pathname, actual.pathname);
  if (expected.origin !== actual.origin || !canonicalRedirect) {
    throw new Error(`Smoke route redirected: expected ${expected.pathname}, loaded ${actual.pathname}`);
  }
  for (const key of new Set(expected.searchParams.keys())) {
    if (canonicalRedirect === "procurement-source-entry" && key === "scope_mode") continue;
    // /verification redirects STRAIGHT to /verify with no query (see the comment in
    // app/(admin)/verification/page.tsx: a two-hop chain through /actions costs a round
    // trip and risks a loop). /actions, the alias that carries deep links, still has to
    // forward every parameter it was given, and is not exempted here.
    if (canonicalRedirect === "verification-verify" && key === "scope_mode") continue;
    if (JSON.stringify(expected.searchParams.getAll(key)) !== JSON.stringify(actual.searchParams.getAll(key))) {
      throw new Error(`Smoke route changed requested query parameter ${key}`);
    }
  }
  return actual.pathname;
}
function canonicalSmokeRedirect(expectedPathname, actualPathname) {
  if (expectedPathname === actualPathname) return "same-path";
  if (expectedPathname === "/sales" && actualPathname === "/sales/sold") return "sales-sold";
  if (expectedPathname === "/procurement" && actualPathname === "/procurement/source-entry") return "procurement-source-entry";
  // Kept-alive links from before the Actions -> Verify rename (maintainer decision
  // 2026-08-12). Both pages exist only to redirect; see
  // app/(admin)/actions/page.tsx and app/(admin)/verification/page.tsx. Landing on
  // /verify is the whole point of visiting them, not a route that went astray.
  if (expectedPathname === "/actions" && actualPathname === "/verify") return "actions-verify";
  if (expectedPathname === "/verification" && actualPathname === "/verify") return "verification-verify";
  return "";
}
export function assertAnimalPurchaseHeading(heading) {
  if (!/^animal purchases$/i.test(heading.trim())) throw new Error('Procurement route did not render the Animal purchases heading');
  return true;
}
