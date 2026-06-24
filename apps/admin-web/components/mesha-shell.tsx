"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import {
  Bell,
  CalendarDays,
  Check,
  ChevronRight,
  Database,
  HeartPulse,
  Menu,
  MapPin,
  Moon,
  Sun,
  TowerControl,
} from "lucide-react";
import { SignOutButton } from "@/components/auth/sign-out-button";

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

const primary: Leaf[] = [{ label: "Control Tower", href: "/" }];
const primaryIcons: Record<string, React.ElementType> = { "/": TowerControl };

const groups: Group[] = [
  {
    // PHC vertical -> Vaccination module. Protocol Rules lives under Admin / Data Ops; Vaccination
    // links to it with category=vaccination only when it needs contextual rule authoring.
    id: "phc",
    label: "PHC",
    icon: HeartPulse,
    defaultOpen: true,
    leaves: [{ label: "Vaccination", href: "/vaccination" }],
  },
  {
    id: "admin-data",
    label: "Admin / Data Ops",
    icon: Database,
    defaultOpen: true,
    leaves: [{ label: "Config", href: "/config" }],
  },
];

const allHrefs: string[] = [...primary, ...groups.flatMap((g) => g.leaves)].map((l) => l.href);

// Module-scope so the client render path stays pure (no new Date in render). The YYYY-MM-DD string is
// stable within a day, so SSR and hydration agree.
function todayIso(): string {
  return new Date().toISOString().slice(0, 10);
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

export function MeshaShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname() ?? "/";
  const active = activeHref(pathname);
  const [navOpen, setNavOpen] = useState(false);
  const [rail, setRail] = useState(false);
  const [isLight, setIsLight] = useState(false);
  const [roleMenuOpen, setRoleMenuOpen] = useState(false);
  const [roleLens, setRoleLens] = useState<RoleLens>(roleLenses[0]);
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    for (const g of groups) {
      init[g.id] = Boolean(g.defaultOpen) || g.leaves.some((l) => l.href === active);
    }
    return init;
  });
  const today = todayIso();

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
        <div className="pscope" title="Scope — park-level selector pending the parks projection">
          <MapPin className="ic" style={{ width: 14 }} />
          <b>All parks</b>
        </div>
        {today ? (
          <div className="pscope" title="Reporting date">
            <CalendarDays className="ic" style={{ width: 14 }} />
            <span className="muted small">as of</span>
            <b>{today}</b>
          </div>
        ) : null}
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
        <div className="userpick">
          <button
            type="button"
            className="me"
            aria-label="Open admin role preview"
            aria-expanded={roleMenuOpen}
            onClick={() => setRoleMenuOpen((open) => !open)}
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
