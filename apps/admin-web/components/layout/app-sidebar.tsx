"use client";

import { useState } from "react";
import {
  Activity,
  BadgeCheck,
  DatabaseZap,
  FileSearch,
  GitBranch,
  PanelLeftClose,
  PanelLeft,
  Search,
  X,
} from "lucide-react";
import { useSidebar } from "./sidebar-context";

const phaseOneItems = [
  { icon: Search, label: "Herd Search" },
  { icon: BadgeCheck, label: "Goat Passport" },
  { icon: DatabaseZap, label: "Import Runs" },
  { icon: FileSearch, label: "Dirty Data Review" },
  { icon: GitBranch, label: "Corrections" },
  { icon: Activity, label: "Analytics Counts" },
];

export function AppSidebar() {
  const [expanded, setExpanded] = useState(true);
  const { mobileOpen, setMobileOpen } = useSidebar();

  const renderItem = (item: {
    icon: React.ElementType;
    label: string;
  }) => {
    const Icon = item.icon;

    return (
      <button
        key={item.label}
        type="button"
        disabled
        title={!expanded ? item.label : undefined}
        onClick={() => setMobileOpen(false)}
        className={`flex cursor-not-allowed items-center gap-3 rounded-lg opacity-60 transition-colors ${
          expanded ? "px-3 h-10" : "justify-center w-10 h-10"
        } text-[#8899AA]`}
      >
        <Icon size={20} className="shrink-0" />
        {expanded && (
          <span className="text-xs font-medium truncate">{item.label}</span>
        )}
      </button>
    );
  };

  const sidebarContent = (
    <aside
      className={`flex h-full flex-col overflow-y-auto sidebar-scroll border-r border-[#334155] bg-[#1A1D24] py-4 transition-all duration-200 ${
        expanded ? "w-[180px] px-3" : "w-[60px] items-center"
      }`}
    >
      {/* Logo + collapse toggle */}
      <div className={`mb-6 flex items-center ${expanded ? "justify-between" : "justify-center"}`}>
        <div className="flex items-center gap-2 shrink-0">
          <div className="flex h-9 w-9 items-center justify-center rounded-full bg-[#14F1D9] text-xs font-black text-[#0F1115]">
            GO
          </div>
          {expanded && (
            <span className="text-sm font-bold text-[#14F1D9]">Goat OS</span>
          )}
        </div>
        {expanded && (
          <button
            onClick={() => setExpanded(false)}
            className="text-[#8899AA] hover:text-[#14F1D9] transition-colors hidden sm:block"
          >
            <PanelLeftClose size={16} />
          </button>
        )}
        {/* Mobile close button */}
        <button
          onClick={() => setMobileOpen(false)}
          className="text-[#8899AA] hover:text-[#14F1D9] transition-colors sm:hidden"
        >
          <X size={18} />
        </button>
      </div>

      {expanded && (
        <p className="text-xs font-bold text-[#14F1D9] mb-4 text-center w-full">Phase 1 Admin</p>
      )}

      {/* Collapse toggle when collapsed (desktop only) */}
      {!expanded && (
        <button
          onClick={() => setExpanded(true)}
          className="hidden sm:flex items-center justify-center w-10 h-10 rounded-lg text-[#8899AA] hover:text-[#14F1D9] hover:bg-[rgba(20,241,217,0.05)] transition-colors mb-2"
          title="Expand sidebar"
        >
          <PanelLeft size={20} />
        </button>
      )}

      <div className={`flex flex-col gap-1 ${expanded ? "" : "items-center"}`}>
        {phaseOneItems.map(renderItem)}
      </div>

      {/* Spacer */}
      <div className="flex-1" />
    </aside>
  );

  return (
    <>
      {/* Desktop sidebar — always visible */}
      <div className="hidden sm:block h-full shrink-0">
        {sidebarContent}
      </div>

      {/* Mobile overlay sidebar */}
      {mobileOpen && (
        <>
          {/* Backdrop */}
          <div
            className="fixed inset-0 z-40 bg-black/60 sm:hidden"
            onClick={() => setMobileOpen(false)}
          />
          {/* Drawer */}
          <div className="fixed inset-y-0 left-0 z-50 sm:hidden">
            {sidebarContent}
          </div>
        </>
      )}
    </>
  );
}
