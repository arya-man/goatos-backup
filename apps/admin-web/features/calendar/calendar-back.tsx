"use client";

import { useRouter } from "next/navigation";
import { ChevronLeft } from "lucide-react";

// Back = real browser history back (mock navBack → history.back()). Calendar is reachable from many places
// (Control Tower, Action Center, Workflows, sidebar nav, a direct/bookmarked URL), so a hardcoded parent
// link would send you to the wrong screen. This returns to wherever you actually came from.
export function CalendarBackButton() {
  const router = useRouter();
  return (
    <button type="button" className="nbback" onClick={() => router.back()} title="Back to the previous screen">
      <ChevronLeft className="ic" aria-hidden="true" /> Back
    </button>
  );
}
