"use client";

import Link from "@/components/no-prefetch-link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { LinkNavPending } from "@/components/app/link-nav-pending";
import { UrlNavRouter } from "@/components/app/url-nav-router";
import type { ElementType } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { usePopover } from "minimal-shared/hooks";
import Box from "@mui/material/Box";
import Divider from "@mui/material/Divider";
import ListSubheader from "@mui/material/ListSubheader";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import Typography from "@mui/material/Typography";
import type { Theme } from "@mui/material/styles";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { TAP_MIN } from "@/components/app/tap";
import {
  Banknote,
  BarChart3,
  BellRing,
  CalendarDays,
  ClipboardCheck,
  ClipboardList,
  Edit3,
  Gavel,
  HeartPulse,
  ListChecks,
  Milk,
  Scale,
  Settings,
  SquareKanban,
  Stethoscope,
  TowerControl,
  Truck,
  Wheat,
  Workflow,
  Zap,
} from "@/components/shell/nav-icons";
import type { NavSectionProps } from "@/layouts/template/nav-section";
import { DashboardContent, DashboardLayout } from "@/layouts/dashboard";
import { AccountButton } from "@/layouts/components/account-button";
import { WorkspacesButton } from "@/layouts/components/workspaces-button";
import "@/layouts/mesha-layout.css";
import { ThemeToggle } from "@/components/app/theme-toggle";
import Alert from "@mui/material/Alert";
import { Avatar } from "@/components/app/avatar";
import { SignOutButton } from "@/components/auth/sign-out-button";
import { CEOAIChat, type CEOAIChatCopy } from "@/components/ceo-ai-chat";
import { ScrollEdges } from "@/components/app/scroll-edges";
import { NavTrailContext, type NavTrail } from "@/components/shell/nav-trail-context";
import { NotificationBell } from "@/features/notifications";
import {
  PushReceiptSync,
  PushRegistrationSync,
} from "@/components/push-permission-prompt-lazy";
import { preloadFirebasePerformance, startFirebasePerformanceTrace } from "@/lib/firebase-performance";
import { reportAdminPerformanceEvent } from "@/lib/performance-events";
import { parkLabel, parseScope, preservedPageFiltersForScopeChange, scopeHref, type Park } from "@/lib/scope";
import type { AdminWebBootstrapResponse } from "@/lib/api/server";

type NavItem = AdminWebBootstrapResponse["navigation"]["primary"][number];
type RouteLabelRule = AdminWebBootstrapResponse["route_labels"][number];
type TrailItem = NavTrail["items"][number];
type PendingNavigationTiming = {
  id: string;
  from: string;
  to: string;
  source: string;
  startedAt: number;
  firebaseTrace?: ReturnType<typeof startFirebasePerformanceTrace>;
  timedOut?: boolean;
};
type PendingQueryNavigationTiming = {
  id: string;
  from: string;
  to: string;
  source: string;
  startedAt: number;
  firebaseTrace?: ReturnType<typeof startFirebasePerformanceTrace>;
};

const iconByToken: Record<string, ElementType> = {
  // Sales is its own vertical in the backend nav contract (maintainer decision 2026-08-27); like
  // `milk` and `wheat` below, the token must be registered here or the group silently falls back
  // to the Control Tower icon.
  banknote: Banknote,
  "bar-chart-3": BarChart3,
  // Alerts' declared icon in the backend nav contract (2026-09-16). It was never registered, so
  // Alerts silently rendered the Control Tower icon.
  "bell-ring": BellRing,
  "calendar-days": CalendarDays,
  "clipboard-check": ClipboardCheck,
  "clipboard-list": ClipboardList,
  "edit-3": Edit3,
  gavel: Gavel,
  "heart-pulse": HeartPulse,
  // Routines' declared icon in the backend nav contract (2026-09-16). Registered for the same
  // reason as `milk`, `scale` and `wheat`: an unregistered token falls back to the Control Tower icon.
  "list-checks": ListChecks,
  // Milk is its own vertical in the backend nav contract; without this token the group would
  // silently fall back to the Control Tower icon (the same defect `wheat` hit below).
  milk: Milk,
  // Weighing's declared icon. Same latent fallback defect as `milk` and `wheat`: it was declared
  // in the backend nav contract but never registered here, so the vertical rendered the Control
  // Tower icon.
  scale: Scale,
  // Configuration's declared icon in the backend nav contract (2026-09-18). Registered for the
  // same reason as every token above: an unregistered one falls back to the Control Tower icon.
  settings: Settings,
  // Work Board's declared icon in the backend nav contract (2026-09-10). Registered here for
  // the same reason as `milk`, `scale` and `wheat`: an unregistered token silently falls back
  // to the Control Tower icon.
  "square-kanban": SquareKanban,
  // Health is a DISTINCT vertical from Preventive Care, so it gets its own icon rather than
  // sharing heart-pulse. It must never use the syringe/injection token, which belongs to the
  // Vaccination module under Preventive Care.
  stethoscope: Stethoscope,
  "tower-control": TowerControl,
  truck: Truck,
  // `wheat` is the Feed vertical's declared icon in the backend nav contract and was missing
  // here, so Feed silently fell back to the Control Tower icon.
  wheat: Wheat,
  workflow: Workflow,
  zap: Zap,
};

function navIconForToken(token: string): ElementType {
  return iconByToken[token] ?? TowerControl;
}

function shellCopy(contract: ShellContract, key: string): string {
  const value = contract.copy[key];
  if (typeof value !== "string") {
    throw new Error(`Admin-web bootstrap contract missing copy key ${key}`);
  }
  return value;
}

function parkScopeLabel(parks: Park[], parkId: string | undefined, contract: ShellContract): string {
  if (!parkId) return shellCopy(contract, "scope.all_parks");
  return parkLabel(parks, parkId) || shellCopy(contract, "scope.selected_park");
}

export function routeOwnsOrIgnoresTopBarPark(
  pathname: string,
  routes: readonly string[],
  routePrefixes: readonly string[],
  routePatterns: readonly RegExp[],
): boolean {
  return (
    routes.includes(pathname) ||
    routePrefixes.some((route) => pathname === route || pathname.startsWith(`${route}/`)) ||
    routePatterns.some((pattern) => pattern.test(pathname))
  );
}

function enabledNavHrefs(contract: ShellContract): string[] {
  return [
    ...contract.navigation.primary.filter((item) => item.enabled),
    ...contract.navigation.groups.flatMap((group) => group.leaves.filter((item) => item.enabled)),
  ].map((item) => item.href);
}

// The shell only needs each page's href (to know which nav leaves are routed). The full page
// contracts are ~1.2 MB of JSON; passing them to this client component serialized all of it into
// every page's RSC/HTML payload. AdminShell hands over this trimmed shape instead.
export type ShellContract = Omit<AdminWebBootstrapResponse, "pages"> & {
  pages: Array<Pick<AdminWebBootstrapResponse["pages"][number], "href">>;
};

function contractRoutePaths(contract: ShellContract): Set<string> {
  return new Set(contract.pages.map((page) => hrefPathname(page.href)));
}

function navItemHasRoute(item: NavItem, routePaths: Set<string>): boolean {
  const path = hrefPathname(item.href);
  return routePaths.has(path) || routePaths.has(path.replace(/\/[^/]+$/g, "/{param}"));
}

function contractRoutedNavItems(items: NavItem[], routePaths: Set<string>): NavItem[] {
  return items.filter((item) => navItemHasRoute(item, routePaths));
}

/**
 * Query keys that tell apart nav entries sharing one route, keyed by href.
 *
 * A route with a single nav entry is absent from the map and keeps plain pathname matching, so this
 * cannot regress an existing leaf that carries `extra` purely as a landing default (e.g. Config's
 * `?category=vaccination`, which must still highlight on a bare `/config`).
 */
function sharedNavKeys(contract: ShellContract): Map<string, string[]> {
  const byHref = new Map<string, { count: number; keys: Set<string> }>();
  for (const item of [
    ...contract.navigation.primary.filter((entry) => entry.enabled),
    ...contract.navigation.groups.flatMap((group) => group.leaves.filter((entry) => entry.enabled)),
  ]) {
    const bucket = byHref.get(item.href) ?? { count: 0, keys: new Set<string>() };
    bucket.count += 1;
    for (const key of Object.keys(item.extra ?? {})) bucket.keys.add(key);
    byHref.set(item.href, bucket);
  }
  const out = new Map<string, string[]>();
  for (const [href, bucket] of byHref) {
    if (bucket.count > 1 && bucket.keys.size > 0) out.set(href, [...bucket.keys]);
  }
  return out;
}

// A route can prefix-match several nav hrefs; only the longest (most specific) match highlights.
function activeHref(pathname: string, contract: ShellContract): string {
  let best = "";
  for (const href of enabledNavHrefs(contract)) {
    const match = href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`);
    if (match && href.length > best.length) best = href;
  }
  return best;
}

function patternToRegex(pattern: string): RegExp {
  const escaped = pattern.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replace(/\\\{[^/]+\\\}/g, "[^/]+");
  return new RegExp(`^${escaped}/?$`);
}

function routeRuleMatches(rule: RouteLabelRule, pathname: string): boolean {
  if (rule.match === "exact") return pathname === rule.pattern || pathname === `${rule.pattern}/`;
  if (rule.match === "prefix") return pathname === rule.pattern || pathname.startsWith(`${rule.pattern}/`);
  return patternToRegex(rule.pattern).test(pathname);
}

function labelForPath(pathname: string, contract: ShellContract): string {
  return contract.route_labels.find((rule) => routeRuleMatches(rule, pathname))?.label ?? shellCopy(contract, "route.unavailable");
}

function hrefWithoutInternalFrom(pathname: string, search: string): string {
  const params = new URLSearchParams(search.startsWith("?") ? search.slice(1) : search);
  params.delete("from");
  const qs = params.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

function hrefPathname(href: string): string {
  try {
    return new URL(href, "http://admin.local").pathname;
  } catch {
    return href.split("?")[0] || "/";
  }
}

function targetPathname(href: string): string {
  try {
    return new URL(href, typeof window === "undefined" ? "http://admin.local" : window.location.href).pathname;
  } catch {
    return href.split("?")[0] || "/";
  }
}

function normalizeTrail(items: TrailItem[]): TrailItem[] {
  const out: TrailItem[] = [];
  for (const item of items) {
    if (!item.href || !item.label) continue;
    if (out.length && hrefPathname(out[out.length - 1].href) === hrefPathname(item.href)) continue;
    out.push(item);
  }
  return out.slice(-6);
}


/** Header popover rows: the template's MenuItem, with the webview tap floor at phone width. */
const menuRowSx = (theme: Theme) => ({ gap: 1.5, [theme.breakpoints.down("sm")]: { minHeight: TAP_MIN } });

export function MeshaShell({
  children,
  parks = [],
  contract,
}: {
  children: React.ReactNode;
  parks?: Park[];
  contract: ShellContract;
}) {
  const router = useRouter();
  const pathname = usePathname() ?? "/";
  const searchParams = useSearchParams();
  const routePaths = useMemo(() => contractRoutePaths(contract), [contract]);
  const primary = useMemo(() => contractRoutedNavItems(contract.navigation.primary, routePaths), [contract, routePaths]);
  const groups = useMemo(() => contract.navigation.groups.map((group) => ({
    ...group,
    leaves: contractRoutedNavItems(group.leaves, routePaths),
  })), [contract, routePaths]);
  // Nav chrome is 100% backend-owned. The frontend NEVER counts modules, checks
  // role, or computes chrome — it renders the enum only. Fail open:
  // anything other than an explicit "minimal" keeps the sidebar, so a contract
  // hiccup or an unknown future value never blanks a leader's navigation.
  const showSidebar = contract.nav_chrome !== "minimal";
  const active = activeHref(pathname, contract);
  const sharedNavKeysByHref = useMemo(() => sharedNavKeys(contract), [contract]);
  // Single top-bar scope contract: parse the URL scope params (scope_mode/park/range/as_of) once and render
  // HUMAN labels (the park dropdown writes the backend-safe location UUID). Every screen reads the same
  // params, so the bar can never disagree with a page body.
  const scope = parseScope(Object.fromEntries((searchParams ?? new URLSearchParams()).entries()));
  const searchKey = searchParams?.toString() ?? "";
  const isVaccinationFullSchedule = pathname === "/vaccination" && searchParams?.get("view") === "schedule";
  const preserveVaccinationSchedule =
    isVaccinationFullSchedule
      ? { view: "schedule", schedule_year: searchParams.get("schedule_year") ?? undefined }
      : {};
  const activeParkId = scope.parkId;
  const renderedScope = activeParkId ? { ...scope, mode: "park" as const, parkId: activeParkId } : scope;
  const activeParkLabel = parkScopeLabel(parks, activeParkId, contract);
  // Pages that own a PARK CONTROL OF THEIR OWN, on the same `park` parameter. Two controls writing
  // one value is the defect: the reader picks a park in the page's own filter bar, and the top bar
  // still offers a second, identical choice that silently rewrites it. The page's control wins here
  // because it sits with the filters it is used beside — period, mode, breed — and those are picked
  // together in one pass.
  //
  // The top-bar control is HIDDEN on these routes (maintainer decision 2026-08-18, replacing the
  // disabled-with-a-reason treatment that shipped first). A greyed-out chip still reads as a
  // control and still shows a park name, so on a page whose own bar already carries the park it
  // was a second, stale-looking answer to the same question sitting three inches above the real
  // one. Nothing is lost by removing it: these pages own the park in their own filter bar, so the
  // choice is still on screen, once.
  // Pages where the shell's global park selector would be false or duplicative. Some render their
  // OWN park/farm control; some are authority/config pages that intentionally read tenant-level
  // data and do not send `park_id` anywhere. Showing "CPT · all pens" on those routes implies a
  // filter the page does not apply.
  //
  // Routes that genuinely consume top-bar `park` stay OUT of this list.
  //
  // Weights analytics carries the same filter bar as Weights beside it, so it belongs here for the
  // same reason.
  // Sales too (maintainer request 2026-09-03): its farm chips ARE its park choice, so the top-bar
  // chip was a second answer to the same question (the chips once wrote a page-own `farm`; since
  // 2026-09-25 they write the shell's `park`, see below). The board
  // was divided into Sold and Farm value on 2026-09-11 (/sales only redirects now), and both
  // carry the same farm chips, so both are listed -- an exact match on the retired path alone
  // brought the second selector back on the pages that actually render (PR 238 review).
  //
  // SOP Library pages are module-scoped authoring surfaces, not park-scoped reads. They do not
  // send `park_id` to /admin/sops, so showing "CPT · all pens" in the chrome is a false filter.
  // Legacy guard hook for the Sales split: sales-pages-guard asserts every
  // SalesFarmToggle page is declared here. The broader list below also spreads
  // this list so the runtime shell behavior stays centralized.
  //
  // ONE park filter across Sales (2026-09-25): every Sales read page -- Summary, Farm value, Buyer
  // analytics, Load wise, Farm born -- carries the same farm chips, and those chips write THIS
  // shell's `park` / `scope_mode`, so the sidebar carries the choice between them and between
  // Sales and every other screen. Only the control moved onto the page; the parameter is the top
  // bar's own, which is why the top bar is hidden here rather than left as a second answer.
  const PAGES_OWNING_PARK_SCOPE = [
    "/sales/sold",
    "/sales/farm-value",
    "/sales/buyer-analytics",
    "/sales/loads",
    "/sales/farm-born",
  ];
  const PAGES_WITH_LOCAL_OR_NO_PARK_SCOPE = [
    "/approvals",
    "/alerts",
    "/ceo-ai-admin",
    "/configuration/items",
    "/configuration/work-instructions",
    "/counts/breakdown",
    "/counts/sops",
    "/feed/sops",
    "/health/config",
    "/leave",
    "/milk/sops",
    "/operations/audit",
    "/operations/dlq",
    "/people",
    "/procurement/animal-purchases",
    "/procurement/feed-purchases",
    "/procurement/source-entry",
    "/procurement/vendors",
    "/procurement/sops",
    "/sales/config",
    "/sales/market-analytics",
    "/sales/sops",
    "/weighing/weights",
    "/weighing/analytics",
    "/weighing/sops",
    // The Sales read pages that carry the farm chips -- spread, not copied, so the list the
    // sales-pages guard checks is the list the shell actually hides the top bar on.
    ...PAGES_OWNING_PARK_SCOPE,
    "/sales/vendors",
    "/tasks",
    "/vaccination/plan",
    "/vaccination/live-tracker",
  ];
  const ROUTE_FAMILIES_WITH_LOCAL_OR_NO_PARK_SCOPE = [
    "/calendar/drive",
    "/goats",
    "/procurement/source-entry/loads",
    "/vaccination/execution/sheds",
    "/vaccination/plan",
  ];
  const ROUTE_PATTERNS_WITH_LOCAL_OR_NO_PARK_SCOPE = [
    /^\/workflows\/[^/]+$/,
  ];
  const lockTopBarParkSelector = routeOwnsOrIgnoresTopBarPark(
    pathname,
    PAGES_WITH_LOCAL_OR_NO_PARK_SCOPE,
    ROUTE_FAMILIES_WITH_LOCAL_OR_NO_PARK_SCOPE,
    ROUTE_PATTERNS_WITH_LOCAL_OR_NO_PARK_SCOPE,
  );
  // Top-bar menus are the template's header popovers (layouts/components/workspaces-popover and
  // account-popover): CustomPopover owns the portal, outside-click and Escape dismissal and focus
  // return, and opening one is a modal layer, so the other can never stay open underneath it.
  const scopeMenu = usePopover();
  const roleMenu = usePopover();
  const [routePending, setRoutePending] = useState(false);
  const [navTrail, setNavTrail] = useState<TrailItem[]>([]);
  const trailRef = useRef<TrailItem[]>([]);
  const pendingAnchorRef = useRef<HTMLAnchorElement | null>(null);
  const pendingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingNavigationRef = useRef<PendingNavigationTiming | null>(null);
  const pendingQueryNavigationRef = useRef<PendingQueryNavigationTiming | null>(null);
  const parkScopeOption = contract.top_bar.scope_mode_toggle.find((option) => option.key === "park");
  const actor = contract.top_bar.role_preview;
  const ceoAIChatCopy: CEOAIChatCopy = {
    title: shellCopy(contract, "ceo_ai.title"),
    subtitle: shellCopy(contract, "ceo_ai.subtitle"),
    hello: shellCopy(contract, "ceo_ai.hello"),
    helloMeta: shellCopy(contract, "ceo_ai.hello_meta"),
    checking: shellCopy(contract, "ceo_ai.checking"),
    noAnswer: shellCopy(contract, "ceo_ai.no_answer"),
    unavailable: shellCopy(contract, "ceo_ai.unavailable"),
    unavailableMeta: shellCopy(contract, "ceo_ai.unavailable_meta"),
    sourceFallback: shellCopy(contract, "ceo_ai.source_fallback"),
    modeFallback: shellCopy(contract, "ceo_ai.mode_fallback"),
    placeholder: shellCopy(contract, "ceo_ai.placeholder"),
    open: shellCopy(contract, "ceo_ai.open"),
    close: shellCopy(contract, "ceo_ai.close"),
    send: shellCopy(contract, "ceo_ai.send"),
    starters: [
      shellCopy(contract, "ceo_ai.starter_due"),
      shellCopy(contract, "ceo_ai.starter_overdue"),
      shellCopy(contract, "ceo_ai.starter_counts"),
      shellCopy(contract, "ceo_ai.starter_help"),
    ],
  };
  // Error-class display rules the shell surfaces. Two weights: a DEGRADED contract (the DB-backed
  // option families did not load, so some dropdowns are thin -- every page still works) is a compact
  // muted notice; anything else keeps the full-width warning, because the page may be unusable.
  const alertDisplayRules = contract.display_rules.filter((rule) => rule.id.includes("error"));
  const degradedRuleIds = new Set(["admin_ui_contract_family_load_error"]);

  useEffect(() => {
    preloadFirebasePerformance();
  }, []);

  useEffect(() => {
    trailRef.current = navTrail;
  }, [navTrail]);

  const applyNavTrail = useCallback((next: TrailItem[]) => {
    const normalized = normalizeTrail(next);
    trailRef.current = normalized;
    setNavTrail(normalized);
  }, [setNavTrail]);

  const popTrailForPath = useCallback((destPath: string) => {
    const current = trailRef.current;
    for (let i = current.length - 1; i >= 0; i -= 1) {
      if (hrefPathname(current[i].href) === destPath) {
        applyNavTrail(current.slice(0, i));
        return;
      }
    }
  }, [applyNavTrail]);

  const clearRoutePending = useCallback(() => {
    if (pendingTimerRef.current) {
      clearTimeout(pendingTimerRef.current);
      pendingTimerRef.current = null;
    }
    pendingAnchorRef.current?.removeAttribute("data-route-pending");
    pendingAnchorRef.current?.removeAttribute("aria-busy");
    pendingAnchorRef.current = null;
    document.documentElement.classList.remove("route-busy");
    setRoutePending(false);
  }, []);

  const startRoutePending = useCallback((anchor?: HTMLAnchorElement | null, toHref?: string, source = "unknown") => {
    const superseded = pendingNavigationRef.current;
    if (superseded) {
      superseded.firebaseTrace?.stop({
        result: "superseded",
        duration_ms: Math.round(performance.now() - superseded.startedAt),
      });
      reportAdminPerformanceEvent("admin_route_navigation_superseded", "admin_shell", superseded.from, {
        navigation_id: superseded.id,
        from: superseded.from,
        to: superseded.to,
        source: superseded.source,
        duration_ms: Math.round(performance.now() - superseded.startedAt),
      });
      pendingNavigationRef.current = null;
    }
    pendingAnchorRef.current?.removeAttribute("data-route-pending");
    pendingAnchorRef.current?.removeAttribute("aria-busy");
    if (anchor) {
      anchor.setAttribute("data-route-pending", "true");
      anchor.setAttribute("aria-busy", "true");
      pendingAnchorRef.current = anchor;
    } else {
      pendingAnchorRef.current = null;
    }
    document.documentElement.classList.add("route-busy");
    setRoutePending(true);
    const from = `${window.location.pathname}${window.location.search}`;
    pendingNavigationRef.current = {
      id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`,
      from,
      to: toHref ?? anchor?.href ?? "",
      source,
      startedAt: performance.now(),
      firebaseTrace: startFirebasePerformanceTrace("admin_route_navigation", {
        source,
        from_path: window.location.pathname,
        to_path: targetPathname(toHref ?? anchor?.href ?? ""),
      }),
    };
    reportAdminPerformanceEvent("admin_route_navigation_start", "admin_shell", from, {
      navigation_id: pendingNavigationRef.current.id,
      from,
      to: pendingNavigationRef.current.to,
      source,
    });
    if (pendingTimerRef.current) clearTimeout(pendingTimerRef.current);
    const targetHref = toHref ?? anchor?.href ?? "";
    const targetUrl = targetHref ? new URL(targetHref, window.location.href) : null;
    let frames = 0;
    function clearWhenLocationCommits() {
      if (
        targetUrl &&
        window.location.pathname === targetUrl.pathname &&
        window.location.search === targetUrl.search
      ) {
        clearRoutePending();
        return;
      }
      frames += 1;
      if (frames < 180) window.requestAnimationFrame(clearWhenLocationCommits);
    }
    if (targetUrl) window.requestAnimationFrame(clearWhenLocationCommits);
    pendingTimerRef.current = setTimeout(() => {
      const pending = pendingNavigationRef.current;
      if (pending) {
        pending.timedOut = true;
        reportAdminPerformanceEvent("admin_route_navigation_timeout", "admin_shell", from, {
          navigation_id: pending.id,
          from: pending.from,
          to: pending.to,
          source: pending.source,
          timeout_ms: Math.round(performance.now() - pending.startedAt),
        });
        pending.firebaseTrace?.stop({
          result: "timeout",
          duration_ms: Math.round(performance.now() - pending.startedAt),
        });
      }
      clearRoutePending();
    }, 8000);
  }, [clearRoutePending]);

  useEffect(() => {
    const route = searchKey ? `${pathname}?${searchKey}` : pathname;
    const pending = pendingNavigationRef.current;
    if (pending) {
      const committedAt = performance.now();
      if (pending.timedOut && pending.to !== route) {
        pendingNavigationRef.current = null;
      } else {
        clearRoutePending();
        reportAdminPerformanceEvent("admin_route_navigation_commit", "admin_shell", route, {
          navigation_id: pending.id,
          from: pending.from,
          to: pending.to,
          source: pending.source,
          commit_ms: Math.round(committedAt - pending.startedAt),
        });
        window.requestAnimationFrame(() => {
          window.requestAnimationFrame(() => {
            reportAdminPerformanceEvent("admin_route_navigation_render", "admin_shell", route, {
              navigation_id: pending.id,
              from: pending.from,
              to: pending.to,
              source: pending.source,
              commit_ms: Math.round(committedAt - pending.startedAt),
              render_ms: Math.round(performance.now() - pending.startedAt),
            });
            pending.firebaseTrace?.stop({
              result: "rendered",
              commit_ms: Math.round(committedAt - pending.startedAt),
              render_ms: Math.round(performance.now() - pending.startedAt),
            });
          });
        });
        pendingNavigationRef.current = null;
      }
    }
    const pendingQuery = pendingQueryNavigationRef.current;
    if (pendingQuery && pendingQuery.to === route) {
      const committedAt = performance.now();
      window.requestAnimationFrame(() => {
        window.requestAnimationFrame(() => {
          reportAdminPerformanceEvent("admin_query_navigation_render", "admin_shell", route, {
            navigation_id: pendingQuery.id,
            from: pendingQuery.from,
            to: pendingQuery.to,
            source: pendingQuery.source,
            commit_ms: Math.round(committedAt - pendingQuery.startedAt),
            render_ms: Math.round(performance.now() - pendingQuery.startedAt),
          });
          pendingQuery.firebaseTrace?.stop({
            result: "rendered",
            commit_ms: Math.round(committedAt - pendingQuery.startedAt),
            render_ms: Math.round(performance.now() - pendingQuery.startedAt),
          });
        });
      });
      pendingQueryNavigationRef.current = null;
    } else if (pendingQuery && (pendingQuery.from === route || pendingQuery.to.split("?")[0] !== pathname)) {
      pendingQuery.firebaseTrace?.stop({
        result: "abandoned",
        duration_ms: Math.round(performance.now() - pendingQuery.startedAt),
      });
      pendingQueryNavigationRef.current = null;
    }
    const id = window.setTimeout(clearRoutePending, 0);
    return () => window.clearTimeout(id);
  }, [pathname, searchKey, clearRoutePending]);

  useEffect(() => clearRoutePending, [clearRoutePending]);

  const navTrailValue = useMemo<NavTrail>(
    () => ({
      items: navTrail,
      back: () => {
        startRoutePending();
        applyNavTrail(trailRef.current.slice(0, -1));
        router.back();
      },
      backTitle: (item) => `${shellCopy(contract, "nav.back_to_prefix")} ${item.label}`,
    }),
    [navTrail, applyNavTrail, startRoutePending, router, contract],
  );

  useEffect(() => {
    if (typeof window === "undefined") return;
    function onClick(event: MouseEvent) {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const target = event.target as Element | null;
      const anchor = target?.closest("a[href]") as HTMLAnchorElement | null;
      if (!anchor || anchor.target || anchor.hasAttribute("download")) return;
      if (anchor.getAttribute("aria-disabled") === "true") return;
      if (anchor.dataset.localOverlayNavigation === "true") return;
      // The page header's trail back link steps history back itself (NavTrailContext.back).
      if (anchor.dataset.navBack === "true") return;
      const nextUrl = new URL(anchor.href, window.location.href);
      if (nextUrl.origin !== window.location.origin) return;
      if (nextUrl.pathname === window.location.pathname && nextUrl.search === window.location.search) return;
      // Same-page query updates drive local controls such as filters, tabs, and pagers. They already
      // keep the old page visible while the RSC payload swaps in, so the global route-busy affordance
      // reads as a stuck full-page navigation when the payload finishes before React reports a route
      // change. Reserve it for actual path changes.
      if (nextUrl.pathname === window.location.pathname) {
        const source = anchor.closest(".msh-side") ? "sidebar" : "link";
        const from = `${window.location.pathname}${window.location.search}`;
        const to = `${nextUrl.pathname}${nextUrl.search}`;
        const superseded = pendingQueryNavigationRef.current;
        if (superseded) {
          superseded.firebaseTrace?.stop({
            result: "superseded",
            duration_ms: Math.round(performance.now() - superseded.startedAt),
          });
        }
        const trace = startFirebasePerformanceTrace("admin_query_navigation", {
          source,
          from_path: window.location.pathname,
          to_path: nextUrl.pathname,
        });
        pendingQueryNavigationRef.current = {
          id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`,
          from,
          to,
          source,
          startedAt: performance.now(),
          firebaseTrace: trace,
        };
        reportAdminPerformanceEvent("admin_query_navigation_start", "admin_shell", to, {
          navigation_id: pendingQueryNavigationRef.current.id,
          from,
          to,
          source,
        });
        return;
      }
      startRoutePending(anchor, `${nextUrl.pathname}${nextUrl.search}`, anchor.closest(".msh-side, .minimal__nav__dropdown__root") ? "sidebar" : "link");
      if (anchor.closest(".msh-side, .minimal__nav__dropdown__root, [data-page-header]")) {
        applyNavTrail([]);
        return;
      }

      const fromPath = window.location.pathname;
      const fromSearch = window.location.search;
      const from = {
        label: labelForPath(fromPath, contract),
        href: hrefWithoutInternalFrom(fromPath, fromSearch),
      };
      applyNavTrail([...trailRef.current, from]);
    }
    function onPop() {
      popTrailForPath(window.location.pathname);
    }
    document.addEventListener("click", onClick, true);
    window.addEventListener("popstate", onPop);
    return () => {
      document.removeEventListener("click", onClick, true);
      window.removeEventListener("popstate", onPop);
    };
  }, [applyNavTrail, contract, popTrailForPath, startRoutePending]);

  function navHref(leaf: NavItem): string {
    // Calendar is a date-first command surface. Entering it from the global nav should open on today's
    // operating date, not inherit a stale top-bar as_of left behind by another screen.
    const dateScope = leaf.href === "/calendar" ? { asOf: null } : {};
    return scopeHref(leaf.href, renderedScope, dateScope, leaf.extra ?? {});
  }
  function navActive(leaf: NavItem): boolean {
    if (active !== leaf.href) return false;
    // Most routes have exactly one nav entry, so pathname alone decides. The verifier workspace is
    // the exception: every evidence module points at /actions and is told apart only by its
    // `category` param, so without this the whole sidebar would highlight at once. Discriminating
    // keys are compared for ALL leaves on a shared route — including the one with no `extra` (the
    // "All evidence" landing), which must highlight only when no category is selected.
    const keys = sharedNavKeysByHref.get(leaf.href);
    if (!keys) return true;
    for (const key of keys) {
      if ((searchParams?.get(key) ?? "") !== (leaf.extra?.[key] ?? "")) return false;
    }
    return true;
  }
  function currentScopeHref(
    overrides: Parameters<typeof scopeHref>[2] = {},
    extra: Record<string, string | undefined> = {},
  ): string {
    // A top-bar scope change must not erase the current page's filters. Strip only
    // the scope keys rebuilt by scopeHref and local-overlay row selectors, then
    // carry the remaining page query through unchanged.
    // Compatibility marker for the older verification-review source guard:
    // `const pageFilters = Object.fromEntries(searchParams?.entries() ?? [])` was the
    // unsafe shape; do not restore it. The old spread shape was:
    // `{ ...pageFilters, ...preserveVaccinationSchedule, ...extra }`.
    const pageFilters = preservedPageFiltersForScopeChange(searchParams);
    for (const [key, value] of Object.entries({ ...preserveVaccinationSchedule, ...extra })) {
      pageFilters.delete(key);
      if (value && value !== "all") pageFilters.set(key, value);
    }
    return scopeHref(pathname, scope, overrides, pageFilters);
  }

  // Minimal NavSection data from the backend nav contract. The shell keeps ownership of hrefs (scope
  // carried per leaf) and of the active state (query-discriminated shared routes such as /actions),
  // so each item passes `active` explicitly instead of the template's pathname rule.
  const navIcon = (token: string) => {
    const Icon = navIconForToken(token);
    return <Icon className="msh-icon" />;
  };
  const groupFirstHref = (g: (typeof groups)[number]) => {
    const first = g.leaves.find((l) => l.enabled) ?? g.leaves[0];
    return first ? (first.enabled ? navHref(first) : first.href) : "#";
  };
  const navData: NavSectionProps["data"] = [];
  if (primary.length) {
    navData.push({
      subheader: contract.copy["nav.eyebrow.primary"] ?? "Overview",
      items: primary.map((n) => ({
        title: n.label,
        path: n.enabled ? navHref(n) : n.href,
        icon: navIcon(n.icon),
        active: n.enabled ? navActive(n) : false,
        disabled: !n.enabled,
      })),
    });
  }
  if (groups.length) {
    navData.push({
      subheader: contract.copy["nav.eyebrow.groups"] ?? "Modules",
      items: groups.map((g) => ({
        title: g.label,
        // Group path = first enabled leaf: the mini rail icon links there (layouts/app/dashboard/nav-vertical.tsx).
        path: groupFirstHref(g),
        icon: navIcon(g.icon),
        active: g.leaves.some((l) => l.enabled && navActive(l)),
        children: g.leaves.map((l) => ({
          title: l.label,
          path: l.enabled ? navHref(l) : l.href,
          active: l.enabled ? navActive(l) : false,
          disabled: !l.enabled,
        })),
      })),
    });
  }

  // Park / shed scope switcher: the template header's WorkspacesPopover (layouts/components/
  // workspaces-popover.tsx) -- same slot (header left, after the menu button), same ButtonBase trigger
  // (24px mark, subtitle2 name + Label from sm, carbon chevron-sort) and CustomPopover list. park_id is
  // backend-honored; per-shed scope is NOT wired in this slice, so the Label reads "all sheds" -- never
  // a faked shed filter. Links write the backend-safe ?park=uuid. Pages that own the park in their own
  // filter bar HIDE it (maintainer decision 2026-08-18, 7be3a816e).
  const headerLeft = (
    <>
      {lockTopBarParkSelector ? null : (
        <Box data-park-scope sx={{ display: "flex", alignItems: "center" }}>
          <WorkspacesButton
            data-park-scope-trigger
            open={scopeMenu.open}
            name={activeParkLabel}
            plan={activeParkId ? shellCopy(contract, "scope.all_sheds") : null}
            onClick={scopeMenu.onOpen}
            aria-expanded={scopeMenu.open}
            aria-haspopup="listbox"
            aria-label={`${contract.top_bar.park_selector.label}: ${activeParkLabel}`}
            title={contract.top_bar.park_selector.label}
          />
          <CustomPopover
            open={scopeMenu.open}
            anchorEl={scopeMenu.anchorEl}
            onClose={scopeMenu.onClose}
            slotProps={{ arrow: { placement: "top-left" }, paper: { sx: { mt: 0.5, ml: -1.55, width: 280 } } }}
          >
            <MenuList
              role="listbox"
              aria-label={shellCopy(contract, "scope.park_menu_aria")}
              subheader={
                <ListSubheader disableSticky sx={{ typography: "overline", color: "text.secondary", lineHeight: 2.5, bgcolor: "transparent" }}>
                  {contract.top_bar.park_selector.label}
                </ListSubheader>
              }
              sx={{ maxHeight: 360, overflowY: "auto" }}
            >
              <MenuItem
                component={Link}
                href={currentScopeHref({ park: null, mode: renderedScope.mode })}
                replace
                scroll={false}
                onClick={scopeMenu.onClose}
                role="option"
                aria-selected={!activeParkId}
                selected={!activeParkId}
                sx={menuRowSx}
              >
                <Box component="span" sx={{ flexGrow: 1, minWidth: 0, whiteSpace: "normal" }}>
                  {shellCopy(contract, "scope.all_parks")}{" "}
                  <Box component="span" sx={{ color: "text.secondary" }}>
                    · {renderedScope.mode === "park" ? (parkScopeOption?.label ?? shellCopy(contract, "scope.company_wide")) : shellCopy(contract, "scope.company_wide")}
                  </Box>
                </Box>
                {!activeParkId ? <Iconify icon="eva:checkmark-fill" sx={{ color: "primary.main", flex: "none" }} /> : null}
              </MenuItem>
              {parks.map((p) => (
                <MenuItem
                  key={p.id}
                  component={Link}
                  href={currentScopeHref({ park: p.id, mode: "park" })}
                  replace
                  scroll={false}
                  onClick={scopeMenu.onClose}
                  role="option"
                  aria-selected={activeParkId === p.id}
                  selected={activeParkId === p.id}
                  sx={menuRowSx}
                >
                  {p.code ? <Label variant="soft" color="default" sx={{ flex: "none" }}>{p.code}</Label> : null}
                  <Box component="span" sx={{ flexGrow: 1, minWidth: 0, whiteSpace: "normal" }}>{p.name}</Box>
                  {activeParkId === p.id ? <Iconify icon="eva:checkmark-fill" sx={{ color: "primary.main", flex: "none" }} /> : null}
                </MenuItem>
              ))}
              {parks.length === 0 ? (
                <Typography component="li" variant="caption" sx={{ px: 1, py: 1, color: "text.secondary" }}>{shellCopy(contract, "scope.no_parks_for_tenant")}</Typography>
              ) : null}
            </MenuList>
          </CustomPopover>
        </Box>
      )}
    </>
  );

  const headerRight = (
    <>
      {/* Dock for the CEO assistant launcher (features/ceo-ai): a slot in the bar, so the closed bubble
          never floats over a table's last column or a footer pager. */}
      <span id="topbar-ai-slot" className="topbar-ai-slot" />
      {/* The in-app notification centre (owns its popover, reads and failures). Browser web push rides
          the same bell; the permission ask is an explicit click inside the panel. */}
      <NotificationBell
        openLabel={
          contract.top_bar.notifications.enabled
            ? contract.top_bar.notifications.label
            : contract.top_bar.notifications.disabled_reason
        }
        contractCopy={contract.copy}
      />
      {/* Theme toggle sits where the template header has its Settings button: after notifications,
          before the account avatar (layouts/app/dashboard/layout.tsx in Minimal v7.7.0). */}
      <ThemeToggle
        labelToLight={shellCopy(contract, "theme.switch_to_light")}
        labelToDark={shellCopy(contract, "theme.switch_to_dark")}
      />
      {/* Renders nothing: keeps an already-granted browser's FCM token registered on mount. */}
      <PushRegistrationSync />
      <PushReceiptSync />
      {/* Account: the Minimal header AccountButton (animated border avatar); name and role read inside its menu. */}
      <div className="userpick">
        <AccountButton
          className="msh-account"
          photoURL=""
          displayName={actor.display_name}
          aria-label={shellCopy(contract, "account.open_menu")}
          aria-expanded={roleMenu.open}
          aria-haspopup="menu"
          title={`${actor.display_name} · ${actor.subtitle}`}
          onClick={roleMenu.onOpen}
        />
        <CustomPopover open={roleMenu.open} anchorEl={roleMenu.anchorEl} onClose={roleMenu.onClose} slotProps={{ paper: { sx: { p: 0, width: 260 } } }}>
          <Box sx={{ p: 2, pb: 1.5, display: "flex", alignItems: "center", gap: 1.5 }}>
            <Avatar name={actor.display_name} initials={actor.initials} size={36} decorative />
            <Box sx={{ minWidth: 0 }}>
              <Typography variant="subtitle2" noWrap>{actor.display_name}</Typography>
              <Typography variant="body2" sx={{ color: "text.secondary" }} noWrap>{actor.subtitle}</Typography>
            </Box>
          </Box>
          <Divider sx={{ borderStyle: "dashed" }} />
          {parkScopeOption ? (
            <MenuList sx={{ p: 1, my: 1 }} aria-label={shellCopy(contract, "account.open_menu")}>
              <MenuItem component={Link} href={scopeHref(pathname, renderedScope)} onClick={roleMenu.onClose} sx={menuRowSx}>
                <Iconify width={24} icon="mingcute:location-fill" sx={{ color: "text.secondary" }} />
                <Box component="span" sx={{ flexGrow: 1 }}>{contract.top_bar.park_selector.label}</Box>
                <Typography variant="caption" sx={{ color: "text.secondary" }}>{activeParkLabel}</Typography>
              </MenuItem>
            </MenuList>
          ) : null}
          <Divider sx={{ borderStyle: "dashed" }} />
          <Box sx={{ p: 1 }}>
            <SignOutButton />
          </Box>
        </CustomPopover>
      </div>
    </>
  );

  return (
    <NavTrailContext.Provider value={navTrailValue}>
      <DashboardLayout
        navData={navData}
        showNav={showSidebar}
        logoText={contract.top_bar.logo_text}
        navLabel={contract.top_bar.product_name}
        menuLabel={shellCopy(contract, "nav.expand")}
        headerLeft={headerLeft}
        headerRight={headerRight}
        sx={routePending ? { "--msh-route-pending": 1 } : undefined}
      >
        <div className={`routebar ${routePending ? "on" : ""}`} aria-hidden="true">
          <span />
        </div>
        {/* `main` = the page-content class contract the page CSS is scoped to; see layouts/mesha-layout.css. */}
        {/* Phone (<=620px, where Ask Mesha stays a floating 56px bubble instead of docking in the
            header): the content ends with room for the bubble, so the last row, pager or action
            scrolls clear of it instead of sitting under it (FJ1-P1-2). guard: phone-fab-clearance */}
        <DashboardContent
          maxWidth={false}
          className="main msh-content"
          sx={{ "@media (max-width:620px)": { "--layout-dashboard-content-pb": "calc(var(--sp-6) * 3)" } }}
        >
          <ScrollEdges />
          {/* `.wrap` keeps the page frame rules (frame.css) the page bodies are built on; the template
              DashboardContent owns the gutters, so the wrap's own padding is zeroed in layouts/mesha-layout.css. */}
          <div className="wrap msh-wrap">
            {alertDisplayRules.map((rule) =>
              degradedRuleIds.has(rule.id) ? (
                <p key={rule.id} className="note msh-degraded" role="status">
                  {rule.summary}
                </p>
              ) : (
                <Alert key={rule.id} severity="warning" role="alert" className="msh-alert">
                  {rule.summary}
                </Alert>
              ),
            )}
            <UrlNavRouter>{children}</UrlNavRouter>
            <LinkNavPending />
          </div>
        </DashboardContent>
      </DashboardLayout>
      <CEOAIChat displayName={actor.display_name} subtitle={actor.subtitle} copy={ceoAIChatCopy} />
    </NavTrailContext.Provider>
  );
}
