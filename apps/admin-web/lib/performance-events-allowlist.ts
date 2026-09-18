// The server-side allow-list for `POST /api/admin-web/performance-events`. The route only logs
// events whose name starts with one of these prefixes and answers 400 `unsupported_event` for
// anything else, so every event name that `reportAdminPerformanceEvent` can send from the shell
// must be covered here -- otherwise the console fills with 400s on every client navigation.
//
// `admin_query_navigation_*` (mesha-shell.tsx: same-route `?query` changes such as a filter chip,
// a row select or the pager) was sent for a long time without being listed, so each of those
// navigations produced two 400s (gate-1 #19, 2026-09-18). It is listed now, and the unit test
// next to this file pins every shell event name against the list.
export const ALLOWED_PERFORMANCE_EVENT_PREFIXES = [
  "feed_config_filter_apply_",
  "admin_route_",
  "admin_query_navigation_",
  "admin_backend_api_",
] as const;

export function isAllowedPerformanceEvent(eventName: string): boolean {
  if (!eventName) return false;
  return ALLOWED_PERFORMANCE_EVENT_PREFIXES.some((prefix) => eventName.startsWith(prefix));
}
