"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { Bell, ChevronRight, LayoutGrid, Menu, MapPin, Moon, Search, Sun, Syringe, TowerControl } from "lucide-react";
import { SignOutButton } from "@/components/auth/sign-out-button";

type Leaf = { label: string; href: string };
type Group = { id: string; label: string; icon: React.ElementType; defaultOpen?: boolean; leaves: Leaf[] };

// Primary global nav — cross-vertical entry points. Goat Passport is a search/utility route, not a
// PHC-flow step.
const primary: Leaf[] = [
  { label: "Control Tower", href: "/" },
  { label: "Goat Passport", href: "/herd" },
];
const primaryIcons: Record<string, React.ElementType> = { "/": TowerControl, "/herd": Search };

// Verticals + operations as collapsible groups. PHC is the modeled vertical; Vaccination nests under
// it (its Action Center / Config / Adherence / Verification screens are in-page subtabs). Legacy
// operational routes are grouped under Operations so they don't dominate the shell.
const groups: Group[] = [
  {
    id: "phc",
    label: "PHC",
    icon: Syringe,
    defaultOpen: true,
    leaves: [{ label: "Vaccination", href: "/vaccination" }],
  },
  {
    id: "ops",
    label: "Operations",
    icon: LayoutGrid,
    leaves: [
      { label: "Counts", href: "/counts" },
      { label: "Locations", href: "/locations" },
      { label: "Operators", href: "/operators" },
      { label: "SOP Library", href: "/sops" },
      { label: "Tasks", href: "/tasks" },
      { label: "Import Review", href: "/import-review" },
      { label: "Data Quality", href: "/data-quality" },
      { label: "Legacy Sync", href: "/legacy-sync" },
      { label: "Mortality", href: "/dashboard/mortality" },
    ],
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
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    for (const g of groups) {
      init[g.id] = Boolean(g.defaultOpen) || g.leaves.some((l) => l.href === active);
    }
    return init;
  });
  const today = todayIso();

  function toggleTheme() {
    document.documentElement.classList.toggle("light");
  }
  function toggleGroup(id: string) {
    setOpenGroups((prev) => ({ ...prev, [id]: !prev[id] }));
  }

  return (
    <>
      <div className="top">
        <div className="iconbtn hamb" onClick={() => setNavOpen((o) => !o)} title="Menu" role="button" tabIndex={0}>
          <Menu className="ic" />
        </div>
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
            <span className="muted small">as of</span>
            <b>{today}</b>
          </div>
        ) : null}
        <div
          className="iconbtn"
          onClick={toggleTheme}
          title="Switch light / dark theme"
          aria-label="Switch light / dark theme"
          role="button"
          tabIndex={0}
        >
          <Sun className="ic theme-sun" />
          <Moon className="ic theme-moon" />
        </div>
        <div className="iconbtn" title="Notifications" aria-label="Notifications" role="button" tabIndex={0}>
          <Bell className="ic" />
        </div>
        <SignOutButton />
      </div>

      <div className={`navscrim ${navOpen ? "on" : ""}`} onClick={() => setNavOpen(false)} />
      <div className="layout">
        <aside className={`side ${navOpen ? "open" : ""}`} id="side">
          {primary.map((n) => {
            const Icon = primaryIcons[n.href] ?? Search;
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
