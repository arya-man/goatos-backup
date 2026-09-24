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
