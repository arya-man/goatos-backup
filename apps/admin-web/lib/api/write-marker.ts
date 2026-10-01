// Read-your-writes across admin-web instances. The short read cache is per process, so a write
// on instance A cannot clear instance B's cache. Instead, every write (server action / route
// handler) sets this cookie on the writer's browser; while it is younger than the cache TTL,
// that browser's reads skip the short cache on every instance and go to the backend.
export const WRITE_MARKER_COOKIE = "goatos_last_write";
export const WRITE_MARKER_WINDOW_MS = 30_000;

export function writeMarkerValue(writtenAtMs: number): string {
  return String(Math.floor(writtenAtMs));
}

export function readBypassesShortCache(marker: string | undefined, nowMs: number): boolean {
  if (!marker || !/^\d+$/.test(marker)) return false;
  const writtenAt = Number(marker);
  // |age| bounds a skewed-clock marker from the future to the same window.
  return Math.abs(nowMs - writtenAt) <= WRITE_MARKER_WINDOW_MS;
}

// POSTs that WRITE NOTHING: a dry-run preview whose body is too large or too structured for a
// query string. They must not clear the read caches or stamp the marker cookie -- setting a cookie
// from a server action makes Next re-render the route, and that re-render pushes the router's own
// URL, dropping the ?tag_sale= param a LocalOverlay drawer lives on. "Tag animals to sale" closed
// itself the moment its Review step loaded (2026-09-25), so no animal could be tagged from the web.
// Keyed by exact backend path; add one only after checking its handler writes nothing.
export const READ_ONLY_BACKEND_POSTS: ReadonlySet<string> = new Set([
  "/admin/goats/sale-allocations/preview",
]);

// POSTs that DO write, but write nothing any short-cached read answers. Only the weighing / growth
// analytics reads go through the short read cache (cachedShortRead in server.ts), so a write that
// cannot change those numbers has nothing to invalidate and no read-your-writes to protect. These
// must not stamp the marker either, for the same Next reason as above.
//
// /verification/review-events is the verifier's video-review TELEMETRY (opened / played / watched),
// flushed from a server action every few seconds while /verify is open. Stamping the marker made
// EVERY flush re-render /verify -- the queue, the bootstrap and the Video Log read again, on a page
// whose speed is the point -- and the re-render's router push dropped the #vi_video_log /
// #vi_feed_verify hash, so a panel opened from its button closed itself seconds later (2026-10-01).
// Add one only after checking no cachedShortRead answer depends on what its handler writes.
export const UNCACHED_EFFECT_BACKEND_POSTS: ReadonlySet<string> = new Set([
  "/verification/review-events",
]);

export function isBackendWrite(method: string, pathname: string): boolean {
  const verb = method.toUpperCase();
  if (verb === "GET" || verb === "HEAD") return false;
  if (verb !== "POST") return true;
  return !(READ_ONLY_BACKEND_POSTS.has(pathname) || UNCACHED_EFFECT_BACKEND_POSTS.has(pathname));
}
