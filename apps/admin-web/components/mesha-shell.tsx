"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import {
  Bell,
  CalendarDays,
  Check,
  ChevronDown,
  ChevronRight,
  ClipboardCheck,
  Database,
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

type Leaf = { label: string; href: string };
type Group = { id: string; label: string; icon: React.ElementType; defaultOpen?: boolean; leaves: Leaf[] };
type RoleLens = { id: string; name: string; scope: string; description: string; superadmin?: boolean };

const roleLenses: RoleLens[] = [
  { id: "coo", name: "Superadmin / COO", scope: "all · deep", description: "R. Teja · Central Command · all parks", superadmin: true },
  { id: "park-head", name: "Park Head · CBE", scope: "all verticals · 1 park", description: "CBE park leadership view" },
  { id: "health-director", name: "Health Director", scope: "vertical · all parks", description: "PHC / health governance view" },
  { id: "health-manager", name: "Health Mgr · CBE", scope: "vertical · 1 park", description: "CBE PHC manager view" },
  { id: "ground", name: "Asst / Ground · CBE", scope: "tasks · 1 park", description: "field execution queue" },
  { id: "investor", name: "Investor", scope: "read-only summary", description: "summary-only lens" },
];

// Top-level command-room screens. The mock makes Control Tower / Action Center / Protocol Adherence /
// Workflows first-class nav, NOT tabs inside a vertical — vaccination-only is the data scope, not the UI
// hierarchy. The verticals (PHC, Parks, Admin) sit below as operational/authoring surfaces.
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

const groups: Group[] = [
  {
    // PHC vertical -> Vaccination operations surface (due drives, sessions, proof/verification shortcuts).
    // Not the Action Center. Protocol Rules lives under Admin / Data Ops; Vaccination links to it with
    // category=vaccination only when it needs contextual rule authoring.
    id: "phc",
    label: "PHC",
    icon: HeartPulse,
    defaultOpen: true,
    leaves: [{ label: "Vaccination", href: "/vaccination" }],
  },
  // Parks is NOT a separate visible vaccination module. The park/shed execution surface
  // (park -> shed -> stage -> drive) renders INSIDE PHC / Vaccination at /vaccination#execution,
  // scoped by the top-bar park dropdown. Parks can power that data via its read-model endpoints, but it
  // does not own a sidebar entry. Park/shed execution renders inside /vaccination#execution.
  {
    // Procurement = its OWN top-level vertical (the goat journey starts at purchase/source, before park
    // arrival). It is OPERATIONAL source-entry only. Control Tower / Action Center / Protocol Adherence /
    // Workflows are top-level command screens and must NOT be nested under a vertical; procurement data
    // surfaces there through the existing top-level routes via ?domain=procurement. Truck (transit/source),
    // never the syringe icon.
    id: "procurement",
    label: "Procurement",
    icon: Truck,
    defaultOpen: false,
    leaves: [{ label: "Source Entry", href: "/procurement/source-entry" }],
  },
  {
    id: "admin-data",
    label: "Admin / Data Ops",
    icon: Database,
    defaultOpen: true,
    leaves: [
      { label: "Config", href: "/config" },
      { label: "SOP Library", href: "/sops" },
    ],
  },
];

const allHrefs: string[] = [...primary, ...groups.flatMap((g) => g.leaves)].map((l) => l.href);

// Business date for the top bar. MUST be the operating-tenant timezone (IST, Asia/Kolkata) — using UTC
// (`toISOString`) shows yesterday after midnight IST (e.g. 00:12 IST = previous UTC day). en-CA gives a
// YYYY-MM-DD string; it's stable within an IST day so SSR and hydration agree.
function todayIso(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata" }).format(new Date());
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
  const pathname = usePathname() ?? "/";
  const searchParams = useSearchParams();
  const active = activeHref(pathname);
  // Single top-bar scope contract: parse the URL scope params (scope_mode/park/range/as_of) once and render
  // HUMAN labels (the park dropdown writes the backend-safe location UUID). Every screen reads the same
  // params, so the bar can never disagree with a page body.
  const scope = parseScope(Object.fromEntries((searchParams ?? new URLSearchParams()).entries()));
  const activeParkLabel = parkLabel(parks, scope.parkId);
  const [navOpen, setNavOpen] = useState(false);
  const [rail, setRail] = useState(false);
  const [isLight, setIsLight] = useState(false);
  const [roleMenuOpen, setRoleMenuOpen] = useState(false);
  const [scopeMenuOpen, setScopeMenuOpen] = useState(false);
  const [rangeMenuOpen, setRangeMenuOpen] = useState(false);
  const [roleLens, setRoleLens] = useState<RoleLens>(roleLenses[0]);
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    for (const g of groups) {
      init[g.id] = Boolean(g.defaultOpen) || g.leaves.some((l) => l.href === active);
    }
    return init;
  });
  const today = todayIso();

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
        {/* Park scope: Company-wide vs a specific park. UI shows the human label; the link writes the
            backend-safe location UUID (?park=<uuid>) that every screen passes to the API as park_id. */}
        <div className="parksel" data-menu-root>
          <button
            type="button"
            className="parkbtn"
            onClick={() => {
              setScopeMenuOpen((o) => !o);
              setRangeMenuOpen(false);
              setRoleMenuOpen(false);
            }}
            aria-expanded={scopeMenuOpen}
            title="Park scope"
          >
            <MapPin className="ic" style={{ width: 14 }} aria-hidden="true" />
            <span>{activeParkLabel}</span>
            <ChevronDown className="ic" style={{ width: 12 }} aria-hidden="true" />
          </button>
          <div className={`parkmenu ${scopeMenuOpen ? "on" : ""}`} role="menu" aria-label="Park scope">
            <div className="pm-label">Scope</div>
            <div className="pm-list">
              <Link href={scopeHref(pathname, scope, { park: null })} className={`pm-item ${!scope.parkId ? "on" : ""}`}>
                <span className="pn">
                  All parks <span className="muted" style={{ fontWeight: 400 }}>· company-wide</span>
                </span>
                {!scope.parkId ? <Check className="ic tick" style={{ width: 14 }} aria-hidden="true" /> : null}
              </Link>
              {parks.map((p) => (
                <Link key={p.id} href={scopeHref(pathname, scope, { park: p.id })} className={`pm-item ${scope.parkId === p.id ? "on" : ""}`}>
                  {p.code ? <span className="pc">{p.code}</span> : null}
                  <span className="pn">{p.name}</span>
                  {scope.parkId === p.id ? <Check className="ic tick" style={{ width: 14 }} aria-hidden="true" /> : null}
                </Link>
              ))}
              {parks.length === 0 ? <div className="pm-hint">No parks available for this tenant.</div> : null}
            </div>
          </div>
        </div>
        {/* As-of date scope. Honored backend params today: park_id + as_of (point-in-time across Control
            Tower, Action Center, Protocol Adherence, Workflows, /vaccination operations, and execution).
            The Last 7 / Last 30 / Custom RANGE control is intentionally DISABLED this pass: no backend
            consumes range/date_from/date_to, so it must not look like it filters.
            TODO(scope-range): wire real range filtering once the due-window semantics are defined, then
            re-enable these options (and restore range links in scopeHref usage). */}
        <div className="parksel" data-menu-root>
          <button
            type="button"
            className="parkbtn"
            onClick={() => {
              setRangeMenuOpen((o) => !o);
              setScopeMenuOpen(false);
              setRoleMenuOpen(false);
            }}
            aria-expanded={rangeMenuOpen}
            title="As-of date scope"
          >
            <CalendarDays className="ic" style={{ width: 14 }} aria-hidden="true" />
            <span>As of {scope.asOf ?? today}</span>
            <ChevronDown className="ic" style={{ width: 12 }} aria-hidden="true" />
          </button>
          <div className={`parkmenu ${rangeMenuOpen ? "on" : ""}`} role="menu" aria-label="As-of date scope">
            <div className="pm-list">
              {/* Shown for context but disabled — no backend filters by range yet, so they must not pretend to. */}
              <span className="pm-item" aria-disabled="true" style={{ opacity: 0.5, cursor: "not-allowed" }}>
                <span className="pn">Last 7 days</span>
                <span className="rl">soon</span>
              </span>
              <span className="pm-item" aria-disabled="true" style={{ opacity: 0.5, cursor: "not-allowed" }}>
                <span className="pn">Last 30 days</span>
                <span className="rl">soon</span>
              </span>
            </div>
            <div className="pm-hint">Results are point-in-time as of the selected date. Range filtering (Last 7 / Last 30 / custom) is not active yet.</div>
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
        <button type="button" className="iconbtn" title="Notifications" aria-label="Notifications">
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
            const Icon = primaryIcons[n.href] ?? TowerControl;
            return (
              <Link
                key={n.href}
                href={n.href}
                className={`nav ${active === n.href ? "on" : ""}`}
                onClick={() => setNavOpen(false)}
              >
                <Icon className="ic" />
                {n.label}
              </Link>
            );
          })}

          {groups.map((g) => {
            const GroupIcon = g.icon;
            const open = openGroups[g.id];
            return (
              <div key={g.id}>
                <div
                  className={`ggrp ${open ? "open" : ""}`}
                  onClick={() => toggleGroup(g.id)}
                  role="button"
                  tabIndex={0}
                  aria-expanded={open}
                >
                  <GroupIcon className="ic" />
                  {g.label}
                  <ChevronRight className="ic chev" />
                </div>
                <div className={`subnav ${open ? "open" : ""}`}>
                  {g.leaves.map((l) => (
                    <Link
                      key={l.href}
                      href={l.href}
                      className={`leaf ${active === l.href ? "on" : ""}`}
                      onClick={() => setNavOpen(false)}
                    >
                      {l.label}
                    </Link>
                  ))}
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
