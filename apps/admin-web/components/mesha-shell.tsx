"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import {
  Bell,
  BarChart3,
  CalendarDays,
  Check,
  ChevronDown,
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
import { parkLabel, parseScope, scopeHref, type Park } from "@/lib/scope";
import { ROLE_LENSES, type RoleLens } from "@/lib/role-lens";

// A nav leaf. Keep the visible app locked to the active vaccination process-integrity slice.
type Leaf = {
  label: string;
  href: string;
  badge?: string;
  disabled?: boolean;
  reason?: string;
  domain?: string;
  extra?: Record<string, string | undefined>;
};
type Group = { id: string; label: string; icon: React.ElementType; defaultOpen?: boolean; badge?: string; leaves: Leaf[] };
type NavCounts = { actionCenter: number | null; phc: number | null };

// Role-preview lenses come from the shared model so the top-bar preview and the Audit Log `Viewing as`
// control can never drift. Preview only — it never bypasses backend RBAC.
const roleLenses = ROLE_LENSES;

// Top-level command-room screens. These are first-class command lenses; their live content stays scoped to
// vaccination process integrity until the product scope is explicitly widened.
const primary: Leaf[] = [
  { label: "Control Tower", href: "/" },
  { label: "Action Center", href: "/action-center" },
  { label: "Protocol Adherence", href: "/protocol-adherence" },
  { label: "Workflows", href: "/workflows" },
];
const primaryIcons: Record<string, React.ElementType> = {
  "/": TowerControl,
  "/action-center": Zap,
  "/protocol-adherence": ClipboardCheck,
  "/workflows": Workflow,
};

function visibleBadge(count: number | null | undefined): string | undefined {
  return typeof count === "number" && count > 0 ? String(count) : undefined;
}

const groups: Group[] = [
  {
    id: "phc",
    label: "PHC",
    icon: HeartPulse,
    defaultOpen: true,
    leaves: [{ label: "Vaccination", href: "/vaccination" }],
  },
  {
    id: "procurement",
    label: "Procurement",
    icon: Truck,
    defaultOpen: false,
    leaves: [{ label: "Source Entry", href: "/procurement/source-entry" }],
  },
  {
    id: "counts",
    label: "Counts",
    icon: BarChart3,
    defaultOpen: false,
    leaves: [{ label: "Herd Register", href: "/counts/herd" }],
  },
  {
    id: "admin-data",
    label: "Admin / Data Ops",
    icon: Edit3,
    defaultOpen: true,
    leaves: [
      { label: "Config", href: "/config", extra: { category: "vaccination" } },
      { label: "Audit Log", href: "/operations/audit" },
      { label: "SOP Library", href: "/sops" },
    ],
  },
];

// Disabled placeholder leaves are not navigable, so they never count toward active-route matching.
const allHrefs: string[] = [...primary, ...groups.flatMap((g) => g.leaves.filter((l) => !l.disabled))].map((l) => l.href);

// Business date for the top bar. MUST be the operating-tenant timezone (IST, Asia/Kolkata) — using UTC
// (`toISOString`) shows yesterday after midnight IST (e.g. 00:12 IST = previous UTC day). en-CA gives a
// YYYY-MM-DD string; it's stable within an IST day so SSR and hydration agree.
function todayIso(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata" }).format(new Date());
}

function dateFreshnessLabel(asOf: string | undefined, today: string): string {
  if (!asOf || asOf === today) return "fresh";
  const asOfTime = Date.parse(`${asOf}T00:00:00+05:30`);
  const todayTime = Date.parse(`${today}T00:00:00+05:30`);
  if (!Number.isFinite(asOfTime) || !Number.isFinite(todayTime)) return "freshness pending";
  const days = Math.max(0, Math.round((todayTime - asOfTime) / 86_400_000));
  return days === 0 ? "fresh" : `${days}d old`;
}

// A route can prefix-match several nav hrefs; only the longest (most specific) match highlights.
function activeHref(pathname: string): string {
  let best = "";
  for (const href of allHrefs) {
    const match = href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`);
    if (match && href.length > best.length) best = href;
  }
  return best;
}

export function MeshaShell({ children, parks = [] }: { children: React.ReactNode; parks?: Park[] }) {
  const router = useRouter();
  const pathname = usePathname() ?? "/";
  const searchParams = useSearchParams();
  const active = activeHref(pathname);
  // Single top-bar scope contract: parse the URL scope params (scope_mode/park/range/as_of) once and render
  // HUMAN labels (the park dropdown writes the backend-safe location UUID). Every screen reads the same
  // params, so the bar can never disagree with a page body.
  const scope = parseScope(Object.fromEntries((searchParams ?? new URLSearchParams()).entries()));
  const defaultPark = parks.find((p) => p.code === "CBE") ?? parks[0] ?? null;
  const explicitScopeMode = Boolean(searchParams?.has("scope_mode"));
  const activeParkId = scope.parkId ?? (!explicitScopeMode ? defaultPark?.id : undefined);
  const renderedScope = activeParkId ? { ...scope, mode: "park" as const, parkId: activeParkId } : scope;
  const activeParkLabel = parkLabel(parks, activeParkId);
  const [navOpen, setNavOpen] = useState(false);
  const [rail, setRail] = useState(false);
  const [isLight, setIsLight] = useState(false);
  const [roleMenuOpen, setRoleMenuOpen] = useState(false);
  const [scopeMenuOpen, setScopeMenuOpen] = useState(false);
  const [rangeMenuOpen, setRangeMenuOpen] = useState(false);
  const [roleLens, setRoleLens] = useState<RoleLens>(roleLenses[0]);
  const [navCounts, setNavCounts] = useState<NavCounts>({ actionCenter: null, phc: null });
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    for (const g of groups) {
      init[g.id] = Boolean(g.defaultOpen) || g.leaves.some((l) => l.href === active);
    }
    return init;
  });
  const today = todayIso();
  const freshness = dateFreshnessLabel(scope.asOf, today);
  const navCountsHref = scopeHref("/api/nav-counts", renderedScope);
  const actionCenterBadge = visibleBadge(navCounts.actionCenter);
  const phcBadge = visibleBadge(navCounts.phc);

  useEffect(() => {
    if (explicitScopeMode || scope.parkId || !defaultPark) return;
    router.replace(scopeHref(pathname, scope, { park: defaultPark.id, mode: "park" }), { scroll: false });
  }, [defaultPark, explicitScopeMode, pathname, router, scope]);

  useEffect(() => {
    let cancelled = false;
    fetch(navCountsHref, { cache: "no-store" })
      .then((response) => (response.ok ? response.json() : null))
      .then((payload: Partial<NavCounts> | null) => {
        if (cancelled) return;
        setNavCounts({
          actionCenter: typeof payload?.actionCenter === "number" ? payload.actionCenter : null,
          phc: typeof payload?.phc === "number" ? payload.phc : null,
        });
      })
      .catch(() => {
        if (!cancelled) setNavCounts({ actionCenter: null, phc: null });
      });
    return () => {
      cancelled = true;
    };
  }, [navCountsHref]);

  // All top-bar dropdowns (park scope, reporting range, role/user) close together: clicking outside any
  // menu root or pressing Escape dismisses them, and opening one closes the others (handled per-button).
  function closeMenus() {
    setScopeMenuOpen(false);
    setRangeMenuOpen(false);
    setRoleMenuOpen(false);
  }
  useEffect(() => {
    if (!scopeMenuOpen && !rangeMenuOpen && !roleMenuOpen) return;
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
  }, [scopeMenuOpen, rangeMenuOpen, roleMenuOpen]);

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
  function navHref(leaf: Leaf): string {
    return scopeHref(leaf.href, renderedScope, leaf.domain ? { domain: leaf.domain } : {}, leaf.extra ?? {});
  }
  function navActive(leaf: Leaf): boolean {
    return active === leaf.href && !leaf.domain && !leaf.extra;
  }

  return (
    <>
      <div className="top">
        <button
          type="button"
          className="iconbtn hamb"
          onClick={toggleNav}
          title={rail ? "Expand navigation" : "Collapse navigation"}
          aria-label={rail ? "Expand navigation" : "Collapse navigation"}
          aria-expanded={!rail}
        >
          <Menu className="ic" />
        </button>
        <div className="brand">
          <span className="logo">मे</span>
          <b style={{ fontSize: 16, letterSpacing: "-.3px" }}>Mesha</b>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {/* Topbar owns park/date scope only. The active module (PHC › Vaccination) is shown by the sidebar
            nav + the page crumb, so no module badge belongs here. Vaccination is a module under PHC, not
            an app-wide scope. */}
        {/* Scope mode toggle — Company-wide (rollup) vs Park-wise (park/shed breakdown). The mock defaults
            to Park-wise CBE when a tenant park is available; the links write the same backend-safe scope
            params as the park picker, so the top bar and every scope-aware screen stay in sync. */}
        <div className="parkpick" style={{ marginRight: 6 }}>
          <Link
            href={scopeHref(pathname, scope, { park: null, mode: "company" })}
            replace
            scroll={false}
            className={renderedScope.mode === "company" ? "on" : ""}
            title="Company-wide rollup across all in-scope parks"
          >
            Company-wide
          </Link>
          <Link
            href={defaultPark ? scopeHref(pathname, scope, { park: activeParkId ?? defaultPark.id, mode: "park" }) : scopeHref(pathname, scope, { mode: "park" })}
            replace
            scroll={false}
            className={renderedScope.mode === "park" ? "on" : ""}
            title={defaultPark ? "Park-wise scope" : "No parks available for park-wise scope"}
            aria-disabled={!defaultPark}
          >
            Park-wise
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
              setRangeMenuOpen(false);
              setRoleMenuOpen(false);
            }}
            aria-expanded={scopeMenuOpen}
            title="Park scope"
          >
            <MapPin className="ic" style={{ width: 14 }} aria-hidden="true" />
            <b>{activeParkLabel}</b>
            {activeParkId ? (
              <span className="muted" style={{ fontWeight: 400 }}>
                · all sheds
              </span>
            ) : null}
            <ChevronDown className="ic" style={{ width: 12 }} aria-hidden="true" />
          </button>
          <div className={`parkmenu ${scopeMenuOpen ? "on" : ""}`} role="menu" aria-label="Park scope">
            <div className="pm-label">Scope</div>
            <div className="pm-list">
              <Link
                href={scopeHref(pathname, scope, { park: null, mode: "company" })}
                replace
                scroll={false}
                onClick={closeMenus}
                className={`pm-item ${!activeParkId ? "on" : ""}`}
              >
                <span className="pn">
                  All parks <span className="muted" style={{ fontWeight: 400 }}>· company-wide</span>
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
              {parks.length === 0 ? <div className="pm-hint">No parks available for this tenant.</div> : null}
            </div>
            <div className="pm-hint">Shed scope: all sheds — per-shed filtering isn’t wired in this slice yet.</div>
          </div>
        </div>
        {/* As-of date scope. Honored backend params today: park_id + as_of (point-in-time across Control
            Tower, Action Center, Protocol Adherence, Workflows, /vaccination operations, and execution).
            The Last 7 / Last 30 / Custom RANGE control is intentionally DISABLED this pass: no backend
            consumes range/date_from/date_to, so it must not look like it filters.
            TODO(scope-range): wire real range filtering once the due-window semantics are defined, then
            re-enable these options (and restore range links in scopeHref usage). */}
        <div className="parksel" data-menu-root style={{ marginRight: 4 }}>
          <button
            type="button"
            className="pscope"
            onClick={() => {
              setRangeMenuOpen((o) => !o);
              setScopeMenuOpen(false);
              setRoleMenuOpen(false);
            }}
            aria-expanded={rangeMenuOpen}
            title="As-of date scope"
          >
            <CalendarDays className="ic" style={{ width: 14 }} aria-hidden="true" />
            <span>Date range:</span>
            <b>Last 30 days</b>
            <span className="muted small" style={{ marginLeft: 2 }}>
              · data {scope.asOf ?? today} · {freshness}
            </span>
            <ChevronDown className="ic" style={{ width: 12 }} aria-hidden="true" />
          </button>
          <div className={`parkmenu ${rangeMenuOpen ? "on" : ""}`} role="menu" aria-label="As-of date scope">
            <div className="pm-label">Date range</div>
            <div className="pm-list">
              {/* Shown for mock parity but DISABLED — no backend filters by range yet, so they must not pretend to. */}
              <span className="pm-item" aria-disabled="true" style={{ opacity: 0.5, cursor: "not-allowed" }}>
                <span className="pn">Last 7 days</span>
                <span className="rl">soon</span>
              </span>
              <span className="pm-item" aria-disabled="true" style={{ opacity: 0.5, cursor: "not-allowed" }}>
                <span className="pn">Last 30 days</span>
                <span className="rl">soon</span>
              </span>
            </div>
            <div className="pm-hint">
              Point-in-time as of {scope.asOf ?? today}. Date-range and the data-freshness (“N days old”)
              indicator aren’t wired to this live-Postgres slice yet.
            </div>
          </div>
        </div>
        <button
          type="button"
          className="iconbtn"
          onClick={toggleTheme}
          title={isLight ? "Switch to dark theme" : "Switch to light theme"}
          aria-label={isLight ? "Switch to dark theme" : "Switch to light theme"}
        >
          {isLight ? <Moon className="ic" /> : <Sun className="ic" />}
        </button>
        <button
          type="button"
          className="iconbtn"
          title="Notifications are not wired in this admin-web slice yet."
          aria-label="Notifications are not wired in this admin-web slice yet"
          disabled
          style={{ opacity: 0.45, cursor: "not-allowed" }}
        >
          <Bell className="ic" />
        </button>
        <div className="userpick" data-menu-root>
          <button
            type="button"
            className="me"
            aria-label="Open admin role preview"
            aria-expanded={roleMenuOpen}
            onClick={() => {
              setRoleMenuOpen((open) => !open);
              setScopeMenuOpen(false);
              setRangeMenuOpen(false);
            }}
          >
            <span className="av">RT</span>
            <span>
              <span className="nm">R. Teja</span>
              <span className="rl">{roleLens.superadmin ? "COO · Command" : roleLens.name}</span>
            </span>
            <ChevronRight className="ic" style={{ width: 14 }} />
          </button>
          <div id="userMenu" className={`parkmenu ${roleMenuOpen ? "on" : ""}`}>
            <div className="role-menu-title">
              <b>R. Teja</b>
              <span>COO · Central Command · all parks</span>
            </div>
            <div className="role-divider" />
            {roleLenses.map((role) => (
              <button
                key={role.id}
                type="button"
                className={`pm-item ${roleLens.id === role.id ? "on" : ""}`}
                onClick={() => {
                  setRoleLens(role);
                  setRoleMenuOpen(false);
                }}
              >
                <span className="pn">{role.name}</span>
                <span className="pr">{role.scope}</span>
                {roleLens.id === role.id ? <Check className="ic tick" /> : null}
              </button>
            ))}
            <div className="role-divider" />
            <SignOutButton />
          </div>
        </div>
      </div>

      <div className={`navscrim ${navOpen ? "on" : ""}`} onClick={() => setNavOpen(false)} />
      <div className={`layout ${rail ? "rail" : ""}`}>
        <aside className={`side ${navOpen ? "open" : ""}`} id="side">
          {primary.map((n) => {
            const Icon = primaryIcons[n.label] ?? primaryIcons[n.href] ?? TowerControl;
            const badge = n.href === "/action-center" ? actionCenterBadge : n.badge;
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
            const GroupIcon = g.icon;
            const open = openGroups[g.id];
            const badge = g.id === "phc" ? phcBadge : g.badge;
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
                    l.disabled ? (
                      <span
                        key={`${g.id}:${l.label}:${l.href}`}
                        className="leaf"
                        aria-disabled="true"
                        title={l.reason}
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
                        {l.badge ? <span className="lct">{l.badge}</span> : null}
                      </Link>
                    ),
                  )}
                </div>
              </div>
            );
          })}

          <div className="grow" />
          <div className="sidefoot">Mesha · goat operating system</div>
        </aside>

        <main className="main">
          <div className="wrap">{children}</div>
        </main>
      </div>
    </>
  );
}
