"use client";

import { usePathname } from "next/navigation";
import { Menu } from "lucide-react";
import { useSidebar } from "./sidebar-context";

function formatSegment(segment: string): string {
  const decoded = decodeURIComponent(segment);
  const upperCaseWords: Record<string, string> = {
    cbe: "CBE",
    cpt: "CPT",
    mis: "MIS",
    "parent-stock": "Parent Stock",
    "milking-mothers": "Milk",
    herd: "Herd Search",
    goats: "Goat Passport",
    counts: "Counts",
    "data-quality": "Data Quality",
    "import-review": "Import Review",
  };
  if (upperCaseWords[decoded.toLowerCase()]) return upperCaseWords[decoded.toLowerCase()];
  return decoded
    .split("-")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

function getBreadcrumb(pathname: string): string {
  const segments = pathname.split("/").filter(Boolean);
  if (segments.length === 0) return "CEO Dashboard";
  if (segments.length === 1 && segments[0] === "counts") return "Counts > Overall";
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
  const { setMobileOpen } = useSidebar();

  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b border-[#334155] bg-[#1A1D24] px-4 sm:px-6">
      <button
        type="button"
        onClick={() => setMobileOpen(true)}
        className="flex sm:hidden h-9 w-9 items-center justify-center rounded-lg text-[#8899AA] hover:bg-[#22262E] hover:text-[#14F1D9] transition-colors shrink-0"
        aria-label="Open navigation"
      >
        <Menu size={18} />
      </button>

      <div className="flex items-center min-w-0 shrink">
        <span className="text-sm font-bold text-[#FFFFFF] truncate max-w-[160px] sm:max-w-none">
          {breadcrumb}
        </span>
      </div>

      <div className="flex-1" />
    </header>
  );
}
