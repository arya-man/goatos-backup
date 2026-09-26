// Pure helpers behind `useUrlTabNav` (kept free of React so they are unit-testable).

/** The window event every URL-driven strip fires on a click; `LinkNavPending` dims the page body on it. */
export const URL_NAV_EVENT = "metricseg:navigate";

export type UrlNavDetail = { value: string; href: string };

type ClickLike = { metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean; button: number; defaultPrevented?: boolean };

/**
 * A click the strip may take over. Modified clicks (new tab, new window, download) and non-primary
 * buttons stay with the browser: the tabs are real links with real hrefs.
 */
export function isPlainLeftClick(event: ClickLike): boolean {
  return !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
}

/**
 * Same path and the same search params (order-insensitive, hash ignored): clicking the tab already
 * shown is not a navigation. `href` resolves against the current URL, as the browser would.
 */
export function isCurrentUrl(href: string, current: { href: string }): boolean {
  let here: URL;
  let target: URL;
  try {
    here = new URL(current.href);
    target = new URL(href, here);
  } catch {
    return false;
  }
  if (target.origin !== here.origin || target.pathname !== here.pathname) return false;
  const sorted = (params: URLSearchParams) => [...params.entries()].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)).map(([k, v]) => `${k}=${v}`).join("&");
  return sorted(target.searchParams) === sorted(here.searchParams);
}

/** The tab to draw as selected: the one just clicked while its navigation is in flight, else the server's. */
export function shownTabValue(value: string, pendingValue: string | null): string {
  return pendingValue ?? value;
}
