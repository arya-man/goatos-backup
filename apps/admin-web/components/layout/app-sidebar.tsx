"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import {
  ArrowLeftRight,
  Baby,
  BarChart3,
  Briefcase,
  Building2,
  ClipboardList,
  DatabaseZap,
  FileSearch,
  HeartPulse,
  LayoutDashboard,
  Milk,
  PanelLeft,
  PanelLeftClose,
  RefreshCw,
  Receipt,
  Search,
  ShoppingCart,
  Stethoscope,
  Syringe,
  TrendingUp,
  Users,
  Wheat,
  X,
} from "lucide-react";
import logoImg from "@/lib/logo.png";
import { useSidebar } from "./sidebar-context";

type NavItem = {
  icon: React.ElementType;
  label: string;
  href?: string;
  status?: "live" | "disabled";
};

const commandItems: NavItem[] = [
  { icon: LayoutDashboard, label: "CEO Dashboard", href: "/" },
  { icon: BarChart3, label: "Counts", href: "/counts" },
  { icon: Search, label: "Herd Search", href: "/herd" },
  { icon: DatabaseZap, label: "Import Review", href: "/import-review" },
  { icon: RefreshCw, label: "Legacy Sync", href: "/legacy-sync" },
  { icon: FileSearch, label: "Data Quality", href: "/data-quality" },
];

const legacyItems: NavItem[] = [
  { icon: HeartPulse, label: "Mortality", status: "disabled" },
  { icon: Baby, label: "Births", status: "disabled" },
  { icon: TrendingUp, label: "Fattening", status: "disabled" },
  { icon: Wheat, label: "Feed", status: "disabled" },
  { icon: Receipt, label: "Sales", status: "disabled" },
  { icon: Briefcase, label: "MIS", status: "disabled" },
  { icon: Building2, label: "Infra", status: "disabled" },
  { icon: Stethoscope, label: "Goats Health", status: "disabled" },
  { icon: Syringe, label: "Vaccination", status: "disabled" },
  { icon: ShoppingCart, label: "Purchase Cost", status: "disabled" },
  { icon: ArrowLeftRight, label: "Shiftings", status: "disabled" },
  { icon: Users, label: "Parent Stock", status: "disabled" },
  { icon: Milk, label: "Milk", status: "disabled" },
  { icon: ClipboardList, label: "Summary", status: "disabled" },
];

export function AppSidebar() {
  const pathname = usePathname();
  const [expanded, setExpanded] = useState(true);
  const { mobileOpen, setMobileOpen } = useSidebar();

  const sidebarContent = (
    <aside
      className={`flex h-full flex-col overflow-y-auto sidebar-scroll border-r border-[#334155] bg-[#1A1D24] py-4 transition-all duration-200 ${
        expanded ? "w-[220px] px-3" : "w-[68px] items-center"
      }`}
    >
      <div className={`flex items-center ${expanded ? "mb-6 justify-between" : "mb-3 justify-center"}`}>
        <Link href="/" className="flex min-w-0 shrink-0 items-center gap-2" onClick={() => setMobileOpen(false)}>
          <Image src={logoImg} alt="Mesha" width={36} height={36} className="rounded-full" priority unoptimized />
          {expanded ? (
            <span className="truncate text-sm font-bold text-[#14F1D9]">Mesha</span>
          ) : null}
        </Link>
        {expanded ? (
          <button
            type="button"
            onClick={() => setExpanded(false)}
            className="hidden rounded-md p-1 text-[#8899AA] transition-colors hover:bg-[rgba(20,241,217,0.08)] hover:text-[#14F1D9] sm:block"
            aria-label="Collapse navigation"
            aria-expanded={expanded}
            title="Collapse menu"
          >
            <PanelLeftClose size={16} />
          </button>
        ) : null}
        <button
          type="button"
          onClick={() => setMobileOpen(false)}
          className="text-[#8899AA] transition-colors hover:text-[#14F1D9] sm:hidden"
          aria-label="Close navigation"
        >
          <X size={18} />
        </button>
      </div>

      {!expanded ? (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="mb-3 hidden h-12 w-12 flex-col items-center justify-center rounded-xl border border-[#14F1D9]/70 bg-[rgba(20,241,217,0.12)] text-[#14F1D9] shadow-[0_0_18px_rgba(20,241,217,0.16)] transition-colors hover:bg-[rgba(20,241,217,0.22)] sm:flex"
          aria-label="Expand menu"
          aria-expanded={expanded}
          title="Expand menu"
        >
          <PanelLeft size={18} />
          <span className="mt-0.5 text-[8px] font-black uppercase tracking-wide">Open</span>
        </button>
      ) : null}

      <NavList expanded={expanded} items={commandItems} pathname={pathname} onNavigate={() => setMobileOpen(false)} />
      <hr className={`my-3 border-[#334155] ${expanded ? "" : "w-8"}`} />
      <NavList expanded={expanded} items={legacyItems} pathname={pathname} onNavigate={() => setMobileOpen(false)} />

      <div className="flex-1" />
    </aside>
  );

  return (
    <>
      <div className="hidden h-full shrink-0 sm:block">{sidebarContent}</div>
      {mobileOpen ? (
        <>
          <div className="fixed inset-0 z-40 bg-black/60 sm:hidden" onClick={() => setMobileOpen(false)} />
          <div className="fixed inset-y-0 left-0 z-50 sm:hidden">{sidebarContent}</div>
        </>
      ) : null}
    </>
  );
}

function NavList({
  expanded,
  items,
  pathname,
  onNavigate,
}: {
  expanded: boolean;
  items: NavItem[];
  pathname: string;
  onNavigate: () => void;
}) {
  return (
    <nav aria-label="Mesha dashboard navigation" className={expanded ? "" : "w-full"}>
      <div className={`flex flex-col gap-1 ${expanded ? "" : "items-center"}`}>
        {items.map((item) => (
          <NavRow key={item.label} expanded={expanded} item={item} pathname={pathname} onNavigate={onNavigate} />
        ))}
      </div>
    </nav>
  );
}

function NavRow({
  expanded,
  item,
  pathname,
  onNavigate,
}: {
  expanded: boolean;
  item: NavItem;
  pathname: string;
  onNavigate: () => void;
}) {
  const Icon = item.icon;
  const href = item.href;
  const disabled = item.status === "disabled" || !href;
  const active = href ? (href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`)) : false;
  const baseClass = expanded
    ? "grid h-10 w-full grid-cols-[32px_minmax(0,1fr)] items-center gap-3 rounded-lg px-3 transition-colors"
    : "flex h-10 w-10 items-center justify-center rounded-lg transition-colors";
  const content = expanded ? (
    <>
      <span className="flex h-7 w-8 items-center justify-center">
        <Icon size={19} className="shrink-0" />
      </span>
      <span className="min-w-0 truncate text-left text-xs font-medium">{item.label}</span>
    </>
  ) : (
    <Icon size={19} className="shrink-0" />
  );

  if (disabled) {
    return (
      <button
        type="button"
        disabled
        title={!expanded ? `${item.label} coming soon` : undefined}
        className={`${baseClass} cursor-not-allowed text-[#8899AA]`}
      >
        {content}
      </button>
    );
  }

  return (
    <Link
      href={href}
      title={!expanded ? item.label : undefined}
      onClick={onNavigate}
      aria-current={active ? "page" : undefined}
      className={`${baseClass} ${
        active
          ? "bg-[rgba(20,241,217,0.08)] text-[#14F1D9]"
          : "text-[#8899AA] hover:bg-[rgba(20,241,217,0.05)] hover:text-[#14F1D9]"
      }`}
    >
      {content}
    </Link>
  );
}
