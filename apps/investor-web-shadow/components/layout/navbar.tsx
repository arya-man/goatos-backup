"use client";

import { usePathname } from "next/navigation";
import { Search, Bell, User } from "lucide-react";

function formatSegment(segment: string): string {
  const upperCaseWords: Record<string, string> = {
    cbe: "CBE",
    cpt: "CPT",
    mis: "MIS",
  };
  if (upperCaseWords[segment.toLowerCase()]) return upperCaseWords[segment.toLowerCase()];
  // Convert "purchase-cost" → "Purchase Cost"
  return segment
    .split("-")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

function getBreadcrumb(pathname: string): string {
  const segments = pathname.split("/").filter(Boolean);

  if (segments.length === 0) return "Dashboard";

  const section = formatSegment(segments[0]);

  if (segments.length > 1) {
    const sub = formatSegment(segments[1]);
    return `${section} > ${sub}`;
  }

  return section;
}

export function Navbar() {
  const pathname = usePathname();
  const breadcrumb = getBreadcrumb(pathname);

  return (
    <header className="flex h-14 shrink-0 items-center border-b border-[#334155] bg-[#1A1D24] px-6">
      {/* Left: Breadcrumb */}
      <div className="flex items-center">
        <span className="text-sm font-bold text-[#FFFFFF]">
          {breadcrumb}
        </span>
      </div>

      {/* Center: Search */}
      <div className="flex flex-1 justify-center px-4">
        <div className="relative w-full max-w-md">
          <Search
            size={16}
            className="absolute left-3 top-1/2 -translate-y-1/2 text-[#8899AA]"
          />
          <input
            type="text"
            placeholder="Search..."
            className="h-9 w-full rounded-lg border border-[#334155] bg-[#22262E] pl-9 pr-14 text-sm text-[#FFFFFF] placeholder-[#8899AA] outline-none focus:border-[#14F1D9] transition-colors"
          />
          <kbd className="absolute right-3 top-1/2 -translate-y-1/2 rounded border border-[#334155] bg-[#0F1115] px-1.5 py-0.5 text-[10px] font-medium text-[#8899AA]">
            ⌘K
          </kbd>
        </div>
      </div>

      {/* Right: Actions */}
      <div className="flex items-center gap-3">
        <button className="flex h-9 w-9 items-center justify-center rounded-lg text-[#8899AA] transition-colors hover:bg-[#22262E] hover:text-[#14F1D9]">
          <Bell size={18} />
        </button>
        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-[#22262E]">
          <User size={16} className="text-[#8899AA]" />
        </div>
      </div>
    </header>
  );
}
