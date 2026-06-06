"use client";

import { useState } from "react";
import Image from "next/image";
import Link from "next/link";
import logoImg from "@/lib/logo.png";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  HeartPulse,
  Baby,
  Wheat,
  Building2,
  ShoppingCart,
  ArrowLeftRight,
  ClipboardList,
  PanelLeftClose,
  PanelLeft,
  Briefcase,
} from "lucide-react";

const livestockItems = [
  { icon: LayoutDashboard, label: "Counts", href: "/counts/overall" },
  { icon: HeartPulse, label: "Mortality", href: "/mortality" },
  { icon: Baby, label: "Births", href: "/births" },
];

const operationsItems = [
  { icon: Wheat, label: "Feed", href: "/feed" },
  { icon: Briefcase, label: "MIS", href: "/mis" },
  { icon: Building2, label: "Infra", href: "/infra" },
  { icon: ShoppingCart, label: "Purchase Cost", href: "/purchase-cost" },
  { icon: ArrowLeftRight, label: "Shiftings", href: "/shiftings" },
  { icon: ClipboardList, label: "Summary", href: "/summary" },
];

export function AppSidebar() {
  const pathname = usePathname();
  const [expanded, setExpanded] = useState(true);

  const isActive = (href: string) => {
    if (href === "/counts/overall") {
      return pathname.startsWith("/counts");
    }
    return pathname === href || pathname.startsWith(href + "/");
  };

  const renderItem = (item: {
    icon: React.ElementType;
    label: string;
    href: string;
  }) => {
    const Icon = item.icon;
    const active = isActive(item.href);

    return (
      <Link
        key={item.href}
        href={item.href}
        title={!expanded ? item.label : undefined}
        className={`flex items-center gap-3 rounded-lg transition-colors ${
          expanded ? "px-3 h-10" : "justify-center w-10 h-10"
        } ${
          active
            ? "bg-[rgba(20,241,217,0.08)] text-[#14F1D9]"
            : "text-[#8899AA] hover:text-[#14F1D9] hover:bg-[rgba(20,241,217,0.05)]"
        }`}
      >
        <Icon size={20} className="shrink-0" />
        {expanded && (
          <span className="text-xs font-medium truncate">{item.label}</span>
        )}
      </Link>
    );
  };

  return (
    <aside
      className={`flex h-full flex-col border-r border-[#334155] bg-[#1A1D24] py-4 transition-all duration-200 ${
        expanded ? "w-[180px] px-3" : "w-[60px] items-center"
      }`}
    >
      {/* Logo + collapse toggle */}
      <div className={`mb-6 flex items-center ${expanded ? "justify-between" : "justify-center"}`}>
        <div className="flex items-center gap-2 shrink-0">
          <Image src={logoImg} alt="VGoat" width={36} height={36} className="rounded-full" />
          {expanded && (
            <span className="text-sm font-bold text-[#14F1D9]">VGoat</span>
          )}
        </div>
        {expanded && (
          <button
            onClick={() => setExpanded(false)}
            className="text-[#8899AA] hover:text-[#14F1D9] transition-colors"
          >
            <PanelLeftClose size={16} />
          </button>
        )}
      </div>

      {expanded && (
        <p className="text-xs font-bold text-[#14F1D9] mb-4 text-center w-full">Dashboard</p>
      )}

      {/* Collapse toggle when collapsed */}
      {!expanded && (
        <button
          onClick={() => setExpanded(true)}
          className="flex items-center justify-center w-10 h-10 rounded-lg text-[#8899AA] hover:text-[#14F1D9] hover:bg-[rgba(20,241,217,0.05)] transition-colors mb-2"
          title="Expand sidebar"
        >
          <PanelLeft size={20} />
        </button>
      )}

      {/* Livestock section */}
      <div className={`flex flex-col gap-1 ${expanded ? "" : "items-center"}`}>
        {livestockItems.map(renderItem)}
      </div>

      {/* Separator */}
      <hr className={`my-3 border-[#334155] ${expanded ? "" : "w-8"}`} />

      {/* Operations section */}
      <div className={`flex flex-col gap-1 ${expanded ? "" : "items-center"}`}>
        {operationsItems.map(renderItem)}
      </div>

      {/* Spacer */}
      <div className="flex-1" />
    </aside>
  );
}
