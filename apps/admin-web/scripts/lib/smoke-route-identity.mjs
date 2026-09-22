export function assertSmokeRouteIdentity(expectedUrl, actualUrl) {
  const expected = new URL(expectedUrl), actual = new URL(actualUrl);
  const canonicalRedirect = canonicalSmokeRedirect(expected.pathname, actual.pathname);
  if (expected.origin !== actual.origin || !canonicalRedirect) {
    throw new Error(`Smoke route redirected: expected ${expected.pathname}, loaded ${actual.pathname}`);
  }
  for (const key of new Set(expected.searchParams.keys())) {
    if (canonicalRedirect === "procurement-source-entry" && key === "scope_mode") continue;
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
  return "";
}
export function assertAnimalPurchaseHeading(heading) {
  if (!/^animal purchases$/i.test(heading.trim())) throw new Error('Procurement route did not render the Animal purchases heading');
  return true;
}
