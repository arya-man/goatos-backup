"use client";

import { useRouter } from "next/navigation";
import { ChevronLeft } from "lucide-react";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Back = real browser history back (mock navBack → history.back()). Calendar is reachable from many places
// (Control Tower, Action Center, Workflows, sidebar nav, a direct/bookmarked URL), so a hardcoded parent
// link would send you to the wrong screen. This returns to wherever you actually came from.
export function CalendarBackButton({ pageContract }: { pageContract: AdminUiPageContract }) {
  const router = useRouter();
  return (
    <button type="button" className="nbback" onClick={() => router.back()} title={copy(pageContract, "action.back_previous")}>
      <ChevronLeft className="ic" aria-hidden="true" /> {copy(pageContract, "action.back")}
    </button>
  );
}
