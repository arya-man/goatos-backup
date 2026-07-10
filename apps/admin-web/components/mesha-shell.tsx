"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import type { ElementType } from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  AlertTriangle,
  Bell,
  BarChart3,
  CalendarDays,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ClipboardCheck,
  Edit3,
  HeartPulse,
  Menu,
  MapPin,
  Moon,
  Sun,
  TowerControl,
  Truck,
  Workflow,
  Zap,
} from "lucide-react";
import { SignOutButton } from "@/components/auth/sign-out-button";
import { todayIso } from "@/lib/format";
import { parkLabel, parseScope, scopeHref, type Park } from "@/lib/scope";
import type { AdminWebBootstrapResponse } from "@/lib/api/server";

type NavItem = AdminWebBootstrapResponse["navigation"]["primary"][number];
type RouteLabelRule = AdminWebBootstrapResponse["route_labels"][number];
type NavCounts = { actionCenter: number | null; pc: number | null };
type TrailItem = { label: string; href: string };

const iconByToken: Record<string, ElementType> = {
  "bar-chart-3": BarChart3,
  "calendar-days": CalendarDays,
  "clipboard-check": ClipboardCheck,
  "edit-3": Edit3,
  "heart-pulse": HeartPulse,
  "tower-control": TowerControl,
  truck: Truck,
  workflow: Workflow,
  zap: Zap,
};

function visibleBadge(count: number | null | undefined): string | undefined {
  return typeof count === "number" && count > 0 ? String(count) : undefined;
}

function badgeForKey(key: string, actionCenterBadge?: string, pcBadge?: string): string | undefined {
  if (key === "action_center_open_work") return actionCenterBadge;
  if (key === "pc_open_work") return pcBadge;
  return undefined;
}

function shellCopy(contract: AdminWebBootstrapResponse, key: string): string {
  const value = contract.copy[key];
  if (typeof value !== "string") {
    throw new Error(`Admin-web bootstrap contract missing copy key ${key}`);
  }
  return value;
}

function dateFreshnessLabel(asOf: string | undefined, today: string, contract: AdminWebBootstrapResponse): string {
  if (!asOf || asOf === today) return shellCopy(contract, "state.fresh");
  const asOfTime = Date.parse(`${asOf}T00:00:00+05:30`);
  const todayTime = Date.parse(`${today}T00:00:00+05:30`);
  if (!Number.isFinite(asOfTime) || !Number.isFinite(todayTime)) return shellCopy(contract, "state.freshness_pending");
  const days = Math.max(0, Math.round((todayTime - asOfTime) / 86_400_000));
  return days === 0 ? shellCopy(contract, "state.fresh") : `${days}${shellCopy(contract, "state.days_old_suffix")}`;
}

function parkScopeLabel(parks: Park[], parkId: string | undefined, contract: AdminWebBootstrapResponse): string {
  if (!parkId) return shellCopy(contract, "scope.all_parks");
  return parkLabel(parks, parkId) || shellCopy(contract, "scope.selected_park");
}

function enabledNavHrefs(contract: AdminWebBootstrapResponse): string[] {
  return [
    ...contract.navigation.primary.filter((item) => item.enabled),
    ...contract.navigation.groups.flatMap((group) => group.leaves.filter((item) => item.enabled)),
  ].map((item) => item.href);
}

// A route can prefix-match several nav hrefs; only the longest (most specific) match highlights.
function activeHref(pathname: string, contract: AdminWebBootstrapResponse): string {
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

function labelForPath(pathname: string, contract: AdminWebBootstrapResponse): string {
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

function normalizeTrail(items: TrailItem[]): TrailItem[] {
  const out: TrailItem[] = [];
  for (const item of items) {
    if (!item.href || !item.label) continue;
    if (out.length && hrefPathname(out[out.length - 1].href) === hrefPathname(item.href)) continue;
    out.push(item);
  }
  return out.slice(-6);
}

export function MeshaShell({
  children,
  parks = [],
  contract,
}: {
  children: React.ReactNode;
  parks?: Park[];
  contract: AdminWebBootstrapResponse;
}) {
  const router = useRouter();
  const pathname = usePathname() ?? "/";
  const searchParams = useSearchParams();
  const primary = contract.navigation.primary;
  const groups = contract.navigation.groups;
  // Nav chrome is 100% backend-owned. The frontend NEVER counts modules, checks
  // role, or computes chrome — it renders the enum only. Fail open:
  // anything other than an explicit "minimal" keeps the sidebar, so a contract
  // hiccup or an unknown future value never blanks a leader's navigation.
  const showSidebar = contract.nav_chrome !== "minimal";
  const active = activeHref(pathname, contract);
  // Single top-bar scope contract: parse the URL scope params (scope_mode/park/range/as_of) once and render
  // HUMAN labels (the park dropdown writes the backend-safe location UUID). Every screen reads the same
  // params, so the bar can never disagree with a page body.
  const scope = parseScope(Object.fromEntries((searchParams ?? new URLSearchParams()).entries()));
  const searchKey = searchParams?.toString() ?? "";
  const defaultPark = parks[0] ?? null;
  const activeParkId = scope.parkId;
  const renderedScope = activeParkId ? { ...scope, mode: "park" as const, parkId: activeParkId } : scope;
  const activeParkLabel = parkScopeLabel(parks, activeParkId, contract);
  const [navOpen, setNavOpen] = useState(false);
  const [rail, setRail] = useState(false);
  const [isLight, setIsLight] = useState(false);
  const [roleMenuOpen, setRoleMenuOpen] = useState(false);
  const [scopeMenuOpen, setScopeMenuOpen] = useState(false);
  const [routePending, setRoutePending] = useState(false);
  const [navCounts, setNavCounts] = useState<NavCounts>({ actionCenter: null, pc: null });
  const [navTrail, setNavTrail] = useState<TrailItem[]>([]);
  const trailRef = useRef<TrailItem[]>([]);
  const pendingAnchorRef = useRef<HTMLAnchorElement | null>(null);
  const pendingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    for (const g of groups) {
      init[g.id] = Boolean(g.default_open) || g.leaves.some((l) => l.href === active);
    }
    return init;
  });
  const today = todayIso();
  const freshness = dateFreshnessLabel(scope.asOf, today, contract);
  const navCountsHref = scopeHref("/api/nav-counts", renderedScope);
  const actionCenterBadge = visibleBadge(navCounts.actionCenter);
  const pcBadge = visibleBadge(navCounts.pc);
  const currentPageLabel = labelForPath(pathname, contract);
  const companyScopeOption = contract.top_bar.scope_mode_toggle.find((option) => option.key === "company");
  const parkScopeOption = contract.top_bar.scope_mode_toggle.find((option) => option.key === "park");
  const actor = contract.top_bar.role_preview;
  const alertDisplayRules = contract.display_rules.filter((rule) => rule.id.includes("error"));

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

  const startRoutePending = useCallback((anchor?: HTMLAnchorElement | null) => {
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
    if (pendingTimerRef.current) clearTimeout(pendingTimerRef.current);
    pendingTimerRef.current = setTimeout(clearRoutePending, 8000);
  }, [clearRoutePending]);

  useEffect(() => {
    const id = window.setTimeout(clearRoutePending, 0);
    return () => window.clearTimeout(id);
  }, [pathname, searchKey, clearRoutePending]);

  useEffect(() => clearRoutePending, [clearRoutePending]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    function onClick(event: MouseEvent) {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const target = event.target as Element | null;
      const anchor = target?.closest("a[href]") as HTMLAnchorElement | null;
      if (!anchor || anchor.target || anchor.hasAttribute("download")) return;
      if (anchor.getAttribute("aria-disabled") === "true") return;
      const nextUrl = new URL(anchor.href, window.location.href);
      if (nextUrl.origin !== window.location.origin) return;
      if (nextUrl.pathname === window.location.pathname && nextUrl.search === window.location.search) return;
      startRoutePending(anchor);
      if (anchor.closest(".navback")) return;

      if (anchor.closest(".side")) {
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

  useEffect(() => {
    let cancelled = false;
    fetch(navCountsHref, { cache: "no-store" })
      .then((response) => (response.ok ? response.json() : null))
      .then((payload: Partial<NavCounts> | null) => {
        if (cancelled) return;
        setNavCounts({
          actionCenter: typeof payload?.actionCenter === "number" ? payload.actionCenter : null,
          pc: typeof payload?.pc === "number" ? payload.pc : null,
        });
      })
      .catch(() => {
        if (!cancelled) setNavCounts({ actionCenter: null, pc: null });
      });
    return () => {
      cancelled = true;
    };
  }, [navCountsHref]);

  // All top-bar dropdowns (park scope, role/user) close together: clicking outside any
  // menu root or pressing Escape dismisses them, and opening one closes the others (handled per-button).
  function closeMenus() {
    setScopeMenuOpen(false);
    setRoleMenuOpen(false);
  }
  useEffect(() => {
    if (!scopeMenuOpen && !roleMenuOpen) return;
    function onDown(e: MouseEvent) {
      const el = e.target as HTMLElement | null;
      if (el && el.closest("[data-menu-root]")) return; // click inside a menu/trigger — its own handler acts
      closeMenus();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") closeMenus();
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [scopeMenuOpen, roleMenuOpen]);

  function toggleTheme() {
    const next = !document.documentElement.classList.contains("light");
    document.documentElement.classList.toggle("light", next);
    setIsLight(next);
  }
  function toggleGroup(id: string) {
    setOpenGroups((prev) => ({ ...prev, [id]: !prev[id] }));
  }
  function toggleNav() {
    if (typeof window !== "undefined" && window.matchMedia("(max-width: 880px)").matches) {
      setNavOpen((o) => !o);
      return;
    }
    setRail((o) => !o);
  }
  function navHref(leaf: NavItem): string {
    return scopeHref(leaf.href, renderedScope, leaf.domain ? { domain: leaf.domain } : {}, leaf.extra ?? {});
  }
  function navActive(leaf: NavItem): boolean {
    return active === leaf.href && !leaf.domain;
  }

  return (
    <>
      <div className="top">
        {showSidebar ? (
          <button
            type="button"
            className="iconbtn hamb"
            onClick={toggleNav}
            title={rail ? shellCopy(contract, "nav.expand") : shellCopy(contract, "nav.collapse")}
            aria-label={rail ? shellCopy(contract, "nav.expand") : shellCopy(contract, "nav.collapse")}
            aria-expanded={!rail}
          >
            <Menu className="ic" />
          </button>
        ) : null}
        <div className="brand">
          <span className="logo">{contract.top_bar.logo_text}</span>
          <b style={{ fontSize: 16, letterSpacing: "-.3px" }}>{contract.top_bar.product_name}</b>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {/* Topbar owns park/date scope only. The active module (Preventive Care (PC) › Vaccination) is shown by the sidebar
            nav + the page crumb, so no module badge belongs here. Vaccination is a module under Preventive Care (PC), not
            an app-wide scope. */}
        {/* Scope mode toggle — Company-wide (rollup) vs Park-wise (park/shed breakdown). Park-wise does
            not force a park filter; users choose a concrete park separately from the park picker. */}
        <div className="parkpick" style={{ marginRight: 6 }}>
          <Link
            href={scopeHref(pathname, scope, { park: null, mode: "company" })}
            replace
            scroll={false}
            className={renderedScope.mode === "company" ? "on" : ""}
            title={companyScopeOption?.title ?? ""}
          >
            {companyScopeOption?.label}
          </Link>
          <Link
            href={defaultPark ? scopeHref(pathname, scope, { park: activeParkId ?? null, mode: "park" }) : scopeHref(pathname, scope)}
            replace
            scroll={false}
            className={renderedScope.mode === "park" ? "on" : ""}
            title={defaultPark ? (parkScopeOption?.title ?? "") : shellCopy(contract, "scope.no_parks_for_park_scope")}
            aria-disabled={!defaultPark}
          >
            {parkScopeOption?.label}
          </Link>
        </div>
        {/* Park / shed scope chip (mock .pscope). park_id is backend-honored; per-shed scope is NOT wired in
            this slice, so the label reads "· all sheds" and the menu disables shed selection with a reason —
            never a faked shed filter. The UI shows the human label; links write the backend-safe ?park=uuid. */}
        <div className="parksel" data-menu-root style={{ marginRight: 4 }}>
          <button
            type="button"
            className="pscope"
            onClick={() => {
              setScopeMenuOpen((o) => !o);
              setRoleMenuOpen(false);
            }}
            aria-expanded={scopeMenuOpen}
            title={contract.top_bar.park_selector.label}
          >
            <MapPin className="ic" style={{ width: 14 }} aria-hidden="true" />
            <b>{activeParkLabel}</b>
            {activeParkId ? (
              <span className="muted" style={{ fontWeight: 400 }}>
                · {shellCopy(contract, "scope.all_sheds")}
              </span>
            ) : null}
            <ChevronDown className="ic" style={{ width: 12 }} aria-hidden="true" />
          </button>
          <div className={`parkmenu ${scopeMenuOpen ? "on" : ""}`} role="menu" aria-label={shellCopy(contract, "scope.park_menu_aria")}>
            <div className="pm-label">{contract.top_bar.park_selector.label}</div>
            <div className="pm-list">
              <Link
                href={scopeHref(pathname, scope, { park: null, mode: renderedScope.mode })}
                replace
                scroll={false}
                onClick={closeMenus}
                className={`pm-item ${!activeParkId ? "on" : ""}`}
              >
                <span className="pn">
                  {shellCopy(contract, "scope.all_parks")}{" "}
                  <span className="muted" style={{ fontWeight: 400 }}>
                    · {renderedScope.mode === "park" ? (parkScopeOption?.label ?? shellCopy(contract, "scope.company_wide")) : shellCopy(contract, "scope.company_wide")}
                  </span>
                </span>
                {!activeParkId ? <Check className="ic tick" style={{ width: 14 }} aria-hidden="true" /> : null}
              </Link>
              {parks.map((p) => (
                <Link
                  key={p.id}
                  href={scopeHref(pathname, scope, { park: p.id, mode: "park" })}
                  replace
                  scroll={false}
                  onClick={closeMenus}
                  className={`pm-item ${activeParkId === p.id ? "on" : ""}`}
                >
                  {p.code ? <span className="pc">{p.code}</span> : null}
                  <span className="pn">{p.name}</span>
                  {activeParkId === p.id ? <Check className="ic tick" style={{ width: 14 }} aria-hidden="true" /> : null}
                </Link>
              ))}
              {parks.length === 0 ? <div className="pm-hint">{shellCopy(contract, "scope.no_parks_for_tenant")}</div> : null}
            </div>
            <div className="pm-hint">{contract.top_bar.park_selector.hint}</div>
          </div>
        </div>
        {/* Point-in-time freshness only. Range filtering is not implemented, so the top bar must not render
            a clickable Date range / Last 30 days control. */}
        <div
          className="pscope"
          style={{ marginRight: 4 }}
          title={`${shellCopy(contract, "date.data_prefix")} ${scope.asOf ?? today} · ${freshness}`}
          aria-label={`${shellCopy(contract, "date.data_prefix")} ${scope.asOf ?? today} · ${freshness}`}
        >
          <CalendarDays className="ic" style={{ width: 14 }} aria-hidden="true" />
          <span>{shellCopy(contract, "date.data_prefix")}</span>
          <b>{scope.asOf ?? today}</b>
          <span className="muted small" style={{ marginLeft: 2 }}>
            · {freshness}
          </span>
        </div>
        <button
          type="button"
          className="iconbtn"
          onClick={toggleTheme}
          title={isLight ? shellCopy(contract, "theme.switch_to_dark") : shellCopy(contract, "theme.switch_to_light")}
          aria-label={isLight ? shellCopy(contract, "theme.switch_to_dark") : shellCopy(contract, "theme.switch_to_light")}
        >
          {isLight ? <Moon className="ic" /> : <Sun className="ic" />}
        </button>
        <button
          type="button"
          className="iconbtn"
          title={contract.top_bar.notifications.disabled_reason}
          aria-label={contract.top_bar.notifications.disabled_reason}
          disabled
          style={{ opacity: 0.45, cursor: "not-allowed" }}
        >
          <Bell className="ic" />
        </button>
        <div className="userpick" data-menu-root>
          <button
            type="button"
            className="me"
            aria-label={shellCopy(contract, "account.open_menu")}
            aria-expanded={roleMenuOpen}
            onClick={() => {
              setRoleMenuOpen((open) => !open);
              setScopeMenuOpen(false);
            }}
          >
            <span className="av">{actor.initials}</span>
            <span>
              <span className="nm">{actor.display_name}</span>
              <span className="rl">{actor.subtitle}</span>
            </span>
            <ChevronRight className="ic" style={{ width: 14 }} />
          </button>
          <div id="userMenu" className={`parkmenu ${roleMenuOpen ? "on" : ""}`}>
            <div className="role-menu-title">
              <b>{actor.display_name}</b>
              <span>{actor.subtitle}</span>
            </div>
            <div className="role-divider" />
            <SignOutButton />
          </div>
        </div>
      </div>
      <div className={`routebar ${routePending ? "on" : ""}`} aria-hidden="true">
        <span />
      </div>

      {showSidebar ? <div className={`navscrim ${navOpen ? "on" : ""}`} onClick={() => setNavOpen(false)} /> : null}
      <div className={`layout ${rail ? "rail" : ""} ${routePending ? "route-pending" : ""}`}>
        {showSidebar ? (
        <aside className={`side ${navOpen ? "open" : ""}`} id="side">
          {primary.map((n) => {
            const Icon = iconByToken[n.icon] ?? TowerControl;
            const badge = badgeForKey(n.badge_key, actionCenterBadge, pcBadge);
            if (!n.enabled) {
              return (
                <span
                  key={`${n.label}:${n.href}`}
                  className="nav"
                  aria-disabled="true"
                  title={n.disabled_reason}
                  style={{ opacity: 0.4, cursor: "not-allowed" }}
                >
                  <Icon className="ic" />
                  {n.label}
                  {badge ? <span className="ct">{badge}</span> : null}
                </span>
              );
            }
            return (
              <Link
                key={`${n.label}:${n.href}`}
                href={navHref(n)}
                className={`nav ${navActive(n) ? "on" : ""}`}
                onClick={() => setNavOpen(false)}
              >
                <Icon className="ic" />
                {n.label}
                {badge ? <span className="ct">{badge}</span> : null}
              </Link>
            );
          })}

          {groups.map((g) => {
            const GroupIcon = iconByToken[g.icon] ?? TowerControl;
            const open = openGroups[g.id];
            const badge = badgeForKey(g.badge_key, actionCenterBadge, pcBadge);
            return (
              <div key={g.id}>
                <div
                  className={`ggrp ${open ? "open" : ""}`}
                  onClick={() => toggleGroup(g.id)}
                  onKeyDown={(event) => {
                    if (event.key !== "Enter" && event.key !== " ") return;
                    event.preventDefault();
                    toggleGroup(g.id);
                  }}
                  role="button"
                  tabIndex={0}
                  aria-expanded={open}
                >
                  <GroupIcon className="ic" />
                  {g.label}
                  {badge ? <span className="gct">{badge}</span> : null}
                  <ChevronRight className="ic chev" />
                </div>
                <div className={`subnav ${open ? "open" : ""}`}>
                  {g.leaves.map((l) =>
                    !l.enabled ? (
                      <span
                        key={`${g.id}:${l.label}:${l.href}`}
                        className="leaf"
                        aria-disabled="true"
                        title={l.disabled_reason}
                        style={{ opacity: 0.4, cursor: "not-allowed" }}
                      >
                        {l.label}
                      </span>
                    ) : (
                      <Link
                        key={`${g.id}:${l.label}:${l.href}`}
                        href={navHref(l)}
                        className={`leaf ${navActive(l) ? "on" : ""}`}
                        onClick={() => setNavOpen(false)}
                      >
                        {l.label}
                        {badgeForKey(l.badge_key, actionCenterBadge, pcBadge) ? (
                          <span className="lct">{badgeForKey(l.badge_key, actionCenterBadge, pcBadge)}</span>
                        ) : null}
                      </Link>
                    ),
                  )}
                </div>
              </div>
            );
          })}

          <div className="grow" />
          <div className="sidefoot">{contract.navigation.footer}</div>
        </aside>
        ) : null}

        <main className="main">
          {navTrail.length ? (
            <div className="navback">
              <button
                type="button"
                className="nbback"
                onClick={() => {
                  startRoutePending();
                  applyNavTrail(navTrail.slice(0, -1));
                  router.back();
                }}
                title={`${shellCopy(contract, "nav.back_to_prefix")} ${navTrail[navTrail.length - 1].label}`}
              >
                <ChevronLeft className="ic" aria-hidden="true" /> {shellCopy(contract, "nav.back")}
              </button>
              <div className="nbtrail">
                {navTrail.map((crumb, index) => (
                  <span key={`${crumb.href}:${index}`} style={{ display: "contents" }}>
                    {index > 0 ? <ChevronRight className="ic nbsep" aria-hidden="true" /> : null}
                    <Link href={crumb.href} className="nbc" onClick={() => applyNavTrail(navTrail.slice(0, index))}>
                      {crumb.label}
                    </Link>
                  </span>
                ))}
                <ChevronRight className="ic nbsep" aria-hidden="true" />
                <span className="nbc cur">{currentPageLabel}</span>
              </div>
            </div>
          ) : null}
          <div className="wrap">
            {alertDisplayRules.map((rule) => (
              <div key={rule.id} className="alert warn" role="alert" style={{ marginBottom: 14 }}>
                <AlertTriangle className="ic" aria-hidden="true" />
                <div>{rule.summary}</div>
              </div>
            ))}
            {children}
          </div>
        </main>
      </div>
    </>
  );
}
