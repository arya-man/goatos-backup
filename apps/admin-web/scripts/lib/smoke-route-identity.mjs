export function assertSmokeRouteIdentity(expectedUrl, actualUrl) {
  const expected = new URL(expectedUrl), actual = new URL(actualUrl);
  if (expected.origin !== actual.origin || expected.pathname !== actual.pathname) {
    throw new Error(`Smoke route redirected: expected ${expected.pathname}, loaded ${actual.pathname}`);
  }
  for (const key of new Set(expected.searchParams.keys())) {
    if (JSON.stringify(expected.searchParams.getAll(key)) !== JSON.stringify(actual.searchParams.getAll(key))) {
      throw new Error(`Smoke route changed requested query parameter ${key}`);
    }
  }
  return actual.pathname;
}
export function assertAnimalPurchaseHeading(heading) {
  if (!/^animal purchases$/i.test(heading.trim())) throw new Error('Procurement route did not render the Animal purchases heading');
  return true;
}
