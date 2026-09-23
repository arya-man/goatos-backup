/** Navigation can revalidate the bell at most once per minute. Explicit reads bypass
 * freshness, while all callers share a pending read. Create one gate per mounted bell;
 * this must never be a module-global cache of authenticated notification data. */
export const NOTIFICATION_NAVIGATION_FRESHNESS_MS = 60_000;

export function createNotificationRefreshGate<T>(
  load: () => Promise<T>,
  now: () => number = Date.now,
) {
  let pending: Promise<T> | undefined;
  let settledAt: number | undefined;

  return {
    load(force = false): Promise<T | undefined> {
      if (pending) return pending;
      if (!force && settledAt !== undefined && now() - settledAt < NOTIFICATION_NAVIGATION_FRESHNESS_MS) {
        return Promise.resolve(undefined);
      }
      pending = Promise.resolve().then(load).finally(() => {
        // Back off navigation after errors too; opening the bell or pressing Refresh
        // always retries immediately. Otherwise an outage turns route visits into a storm.
        settledAt = now();
        pending = undefined;
      });
      return pending;
    },
  };
}
