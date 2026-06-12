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
  Milk,
  PanelLeft,
  PanelLeftClose,
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
  { icon: BarChart3, label: "Counts", href: "/counts" },
  { icon: Search, label: "Herd Search", href: "/herd" },
  { icon: DatabaseZap, label: "Import Review", href: "/import-review" },
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
        expanded ? "w-[180px] px-3" : "w-[60px] items-center"
      }`}
    >
      <div className={`mb-6 flex items-center ${expanded ? "justify-between" : "justify-center"}`}>
        <Link href="/" className="flex min-w-0 shrink-0 items-center gap-2" onClick={() => setMobileOpen(false)}>
          <Image src={logoImg} alt="Mesha" width={36} height={36} className="rounded-full" priority />
          {expanded ? (
            <span className="truncate text-sm font-bold text-[#14F1D9]">Mesha</span>
          ) : null}
        </Link>
        {expanded ? (
          <button
            type="button"
            onClick={() => setExpanded(false)}
            className="hidden text-[#8899AA] transition-colors hover:text-[#14F1D9] sm:block"
            aria-label="Collapse navigation"
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
          className="mb-2 hidden h-10 w-10 items-center justify-center rounded-lg text-[#8899AA] transition-colors hover:bg-[rgba(20,241,217,0.05)] hover:text-[#14F1D9] sm:flex"
          aria-label="Expand navigation"
        >
          <PanelLeft size={20} />
        </button>
      ) : null}

      {expanded ? <p className="mb-4 w-full text-center text-xs font-bold text-[#14F1D9]">CEO Dashboard</p> : null}

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
  const baseClass = `flex h-10 items-center gap-3 rounded-lg transition-colors ${
    expanded ? "px-3" : "w-10 justify-center"
  }`;
  const content = (
    <>
      <Icon size={19} className="shrink-0" />
      {expanded ? (
        <span className="min-w-0 flex-1 truncate text-xs font-medium">{item.label}</span>
      ) : null}
    </>
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
