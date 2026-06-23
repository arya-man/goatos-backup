"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import {
  BarChart3,
  Bell,
  ClipboardList,
  DatabaseZap,
  FileSearch,
  GitBranch,
  HeartPulse,
  ListChecks,
  MapPinned,
  Menu,
  Moon,
  RefreshCw,
  Search,
  Sun,
  Syringe,
  TowerControl,
  Users,
  Zap,
} from "lucide-react";
import { SignOutButton } from "@/components/auth/sign-out-button";

type NavLeaf = { label: string; href: string; icon: React.ElementType; badge?: string };

// Primary command nav — only routes that exist today.
const primary: NavLeaf[] = [
  { label: "Control Tower", href: "/", icon: TowerControl },
  { label: "Action Center", href: "/vaccination", icon: Zap },
  { label: "Protocol Adherence", href: "/vaccination/adherence", icon: GitBranch },
  { label: "Config — Schedule", href: "/vaccination/config", icon: ClipboardList },
  { label: "Goat Passport", href: "/herd", icon: Search },
];

// Operational modules — existing live routes.
const modules: NavLeaf[] = [
  { label: "Counts", href: "/counts", icon: BarChart3 },
  { label: "Locations", href: "/locations", icon: MapPinned },
  { label: "Operators", href: "/operators", icon: Users },
  { label: "SOP Builder", href: "/sops", icon: ClipboardList },
  { label: "Tasks", href: "/tasks", icon: ListChecks },
  { label: "Import Review", href: "/import-review", icon: DatabaseZap },
  { label: "Legacy Sync", href: "/legacy-sync", icon: RefreshCw },
  { label: "Data Quality", href: "/data-quality", icon: FileSearch },
  { label: "Mortality", href: "/dashboard/mortality", icon: HeartPulse },
];

const allHrefs: string[] = [...primary, ...modules].map((n) => n.href);

// A route can prefix-match several nav hrefs (e.g. /vaccination/adherence matches both /vaccination
// and /vaccination/adherence). Only the LONGEST (most specific) match should highlight.
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

  function toggleTheme() {
    document.documentElement.classList.toggle("light");
  }

  return (
    <>
      {/* Top bar */}
      <div className="top">
        <div className="iconbtn hamb" onClick={() => setNavOpen((o) => !o)} title="Menu" role="button" tabIndex={0}>
          <Menu className="ic" />
        </div>
        <div className="brand">
          <span className="logo">मे</span>
          <b style={{ fontSize: 19, letterSpacing: "-.3px" }}>Mesha</b>
        </div>
        <div style={{ flex: 1 }} />
        <div className="pscope" title="Scope">
          <MapPinned className="ic" style={{ width: 14 }} />
          <b>All parks</b>
        </div>
        <div className="iconbtn" onClick={toggleTheme} title="Toggle light / dark" role="button" tabIndex={0}>
          <Sun className="ic theme-sun" />
          <Moon className="ic theme-moon" />
        </div>
        <div className="iconbtn" title="Notifications" role="button" tabIndex={0}>
          <Bell className="ic" />
        </div>
        <SignOutButton />
      </div>

      <div className={`navscrim ${navOpen ? "on" : ""}`} onClick={() => setNavOpen(false)} />
      <div className="layout">
        <aside className={`side ${navOpen ? "open" : ""}`} id="side">
          {primary.map((n) => {
            const Icon = n.icon;
            return (
              <Link
                key={n.href}
                href={n.href}
                className={`nav ${active === n.href ? "on" : ""}`}
                onClick={() => setNavOpen(false)}
              >
                <Icon className="ic" />
                {n.label}
                {n.badge ? <span className="ct">{n.badge}</span> : null}
              </Link>
            );
          })}

          <div className="ggrp" style={{ pointerEvents: "none" }}>
            <Syringe className="ic" />
            Modules
          </div>
          <div className="subnav" data-grp="modules">
            {modules.map((n) => (
              <Link
                key={n.href}
                href={n.href}
                className={`leaf ${active === n.href ? "on" : ""}`}
                onClick={() => setNavOpen(false)}
              >
                {n.label}
              </Link>
            ))}
          </div>
        </aside>

        <main className="main">
          <div style={{ padding: "clamp(14px, 2vw, 22px)" }}>{children}</div>
        </main>
      </div>
    </>
  );
}
