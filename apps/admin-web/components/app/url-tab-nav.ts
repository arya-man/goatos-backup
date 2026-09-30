// Pure helpers behind `useUrlTabNav` (kept free of React so they are unit-testable).

/** The window event every URL-driven control fires as it navigates: `UrlPanel` (UrlSuspense) swaps its panel to the skeleton on it. */
export const URL_NAV_EVENT = "url-nav:navigate";

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

/** `watch` entry meaning "every search param" (minus `ignore`): a panel whose one read takes them all. */
export const ALL_PARAMS = "*";

type SearchLike = string | URLSearchParams | Record<string, string | string[] | undefined>;

function paramKeys(search: SearchLike): string[] {
  if (typeof search === "string" || search instanceof URLSearchParams) {
    return [...new Set((typeof search === "string" ? new URLSearchParams(search) : search).keys())];
  }
  return Object.keys(search).filter((key) => search[key] != null);
}

/**
 * Every value of each watched param, in a stable order: the identity of a URL-keyed panel.
 * `watch` may contain `ALL_PARAMS` ("*"): every param present on either side, minus `ignore`
 * (drawer / overlay / export params that never change the panel's data).
 */
export function watchedParamsKey(search: SearchLike, watch: readonly string[], ignore: readonly string[] = [], extra: readonly string[] = []): string {
  const read = (key: string): string[] => {
    if (typeof search === "string" || search instanceof URLSearchParams) {
      return (typeof search === "string" ? new URLSearchParams(search) : search).getAll(key);
    }
    const value = search[key];
    return value == null ? [] : Array.isArray(value) ? value : [value];
  };
  const keys = watch.includes(ALL_PARAMS)
    ? [...new Set([...watch.filter((key) => key !== ALL_PARAMS), ...paramKeys(search), ...extra])].filter((key) => !ignore.includes(key)).sort()
    : [...watch];
  return keys.map((key) => `${key}=${read(key).join(",")}`).join("&");
}

/** Whether navigating to `href` changes any watched param of the current URL (same page only). */
export function changesWatchedParams(href: string, current: { href: string }, watch: readonly string[], ignore: readonly string[] = []): boolean {
  let here: URL;
  let target: URL;
  try {
    here = new URL(current.href);
    target = new URL(href, here);
  } catch {
    return false;
  }
  if (target.origin !== here.origin || target.pathname !== here.pathname) return false;
  // Both sides over the union of their keys, so a param that appears or disappears counts.
  const union = watch.includes(ALL_PARAMS) ? [...new Set([...target.searchParams.keys(), ...here.searchParams.keys()])] : [];
  return watchedParamsKey(target.searchParams, watch, ignore, union) !== watchedParamsKey(here.searchParams, watch, ignore, union);
}

/** Tell the page a URL navigation to `href` has started (tabs, chips, filters, pagers). */
export function announceUrlNav(href: string, value = ""): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent<UrlNavDetail>(URL_NAV_EVENT, { detail: { value, href } }));
}
