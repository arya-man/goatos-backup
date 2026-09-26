"use client";

import { usePathname } from "next/navigation";
import type { ComponentType, ReactNode } from "react";
import { GenericPageSkeleton } from "@/components/shell-skeleton";

// GENERATED-BY-HAND registry of every route loading.tsx, keyed by the route it serves. The admin
// layout's Suspense fallback renders the SAME skeleton the route's own loading.tsx will render, so a
// direct load paints one continuous skeleton (shell chrome + route shape) instead of a generic page
// skeleton that is then replaced by the route's (the double-skeleton the integrity lane reports).
// Longest route prefix wins; dynamic segments match any single path segment. Assistant-owned routes
// (ceo-ai-admin) are deliberately NOT registered (ceo-ai boundary): they fall back to the generic skeleton.

import L0 from "@/app/(admin)/procurement/source-entry/loads/[load_id]/loading";
import L1 from "@/app/(admin)/vaccination/execution/sheds/[shedId]/loading";
import L2 from "@/app/(admin)/procurement/animal-purchases/loading";
import L3 from "@/app/(admin)/procurement/feed-purchases/loading";
import L4 from "@/app/(admin)/calendar/drive/[eventId]/loading";
import L5 from "@/app/(admin)/vaccination/live-tracker/loading";
import L6 from "@/app/(admin)/procurement/source-entry/loading";
import L7 from "@/app/(admin)/counts/milk-preparation/loading";
import L8 from "@/app/(admin)/sales/market-analytics/loading";
import L9 from "@/app/(admin)/sales/buyer-analytics/loading";
import L10 from "@/app/(admin)/vaccination/plan/edit/loading";
import L11 from "@/app/(admin)/procurement/vendors/loading";
import L12 from "@/app/(admin)/workflows/[row_id]/loading";
import L13 from "@/app/(admin)/weighing/analytics/loading";
import L14 from "@/app/(admin)/protocol-adherence/loading";
import L15 from "@/app/(admin)/sales/farm-value/loading";
import L16 from "@/app/(admin)/health/analytics/loading";
import L17 from "@/app/(admin)/operations/audit/loading";
import L18 from "@/app/(admin)/vaccination/plan/loading";
import L19 from "@/app/(admin)/procurement/sops/loading";
import L20 from "@/app/(admin)/weighing/weights/loading";
import L21 from "@/app/(admin)/counts/breakdown/loading";
import L22 from "@/app/(admin)/counts/mortality/loading";
import L23 from "@/app/(admin)/counts/analytics/loading";
import L24 from "@/app/(admin)/goats/[goat_id]/loading";
import L25 from "@/app/(admin)/sales/farm-born/loading";
import L26 from "@/app/(admin)/operations/dlq/loading";
import L27 from "@/app/(admin)/feed/direction/loading";
import L28 from "@/app/(admin)/feed/analytics/loading";
import L29 from "@/app/(admin)/sales/vendors/loading";
import L30 from "@/app/(admin)/health/config/loading";
import L31 from "@/app/(admin)/action-center/loading";
import L32 from "@/app/(admin)/weighing/sops/loading";
import L34 from "@/app/(admin)/sales/config/loading";
import L35 from "@/app/(admin)/feed/packing/loading";
import L36 from "@/app/(admin)/herd-signals/loading";
import L37 from "@/app/(admin)/sales/loads/loading";
import L38 from "@/app/(admin)/vaccination/loading";
import L39 from "@/app/(admin)/feed/config/loading";
import L40 from "@/app/(admin)/counts/herd/loading";
import L41 from "@/app/(admin)/counts/sops/loading";
import L42 from "@/app/(admin)/work-board/loading";
import L43 from "@/app/(admin)/sales/sold/loading";
import L44 from "@/app/(admin)/milk/sops/loading";
import L45 from "@/app/(admin)/workflows/loading";
import L46 from "@/app/(admin)/feed/sops/loading";
import L47 from "@/app/(admin)/approvals/loading";
import L48 from "@/app/(admin)/calendar/loading";
import L49 from "@/app/(admin)/routines/loading";
import L50 from "@/app/(admin)/weighing/loading";
import L51 from "@/app/(admin)/verify/loading";
import L52 from "@/app/(admin)/alerts/loading";
import L53 from "@/app/(admin)/people/loading";
import L54 from "@/app/(admin)/tasks/loading";
import L56 from "@/app/(admin)/leave/loading";
import L57 from "@/app/(admin)/vaccination/care-coverage/loading";

const ROUTE_SKELETONS: Array<[RegExp, ComponentType]> = [
  [/^\/procurement\/source-entry\/loads\/[^/]+(?:\/|$)/, L0],
  [/^\/vaccination\/execution\/sheds\/[^/]+(?:\/|$)/, L1],
  [/^\/procurement\/animal-purchases(?:\/|$)/, L2],
  [/^\/procurement\/feed-purchases(?:\/|$)/, L3],
  [/^\/calendar\/drive\/[^/]+(?:\/|$)/, L4],
  [/^\/vaccination\/live-tracker(?:\/|$)/, L5],
  [/^\/vaccination\/care-coverage(?:\/|$)/, L57],
  [/^\/procurement\/source-entry(?:\/|$)/, L6],
  [/^\/counts\/milk-preparation(?:\/|$)/, L7],
  [/^\/sales\/market-analytics(?:\/|$)/, L8],
  [/^\/sales\/buyer-analytics(?:\/|$)/, L9],
  [/^\/vaccination\/plan\/edit(?:\/|$)/, L10],
  [/^\/procurement\/vendors(?:\/|$)/, L11],
  [/^\/workflows\/[^/]+(?:\/|$)/, L12],
  [/^\/weighing\/analytics(?:\/|$)/, L13],
  [/^\/protocol-adherence(?:\/|$)/, L14],
  [/^\/sales\/farm-value(?:\/|$)/, L15],
  [/^\/health\/analytics(?:\/|$)/, L16],
  [/^\/operations\/audit(?:\/|$)/, L17],
  [/^\/vaccination\/plan(?:\/|$)/, L18],
  [/^\/procurement\/sops(?:\/|$)/, L19],
  [/^\/weighing\/weights(?:\/|$)/, L20],
  [/^\/counts\/breakdown(?:\/|$)/, L21],
  [/^\/counts\/mortality(?:\/|$)/, L22],
  [/^\/counts\/analytics(?:\/|$)/, L23],
  [/^\/goats\/[^/]+(?:\/|$)/, L24],
  [/^\/sales\/farm-born(?:\/|$)/, L25],
  [/^\/operations\/dlq(?:\/|$)/, L26],
  [/^\/feed\/direction(?:\/|$)/, L27],
  [/^\/feed\/analytics(?:\/|$)/, L28],
  [/^\/sales\/vendors(?:\/|$)/, L29],
  [/^\/health\/config(?:\/|$)/, L30],
  [/^\/action-center(?:\/|$)/, L31],
  [/^\/weighing\/sops(?:\/|$)/, L32],
  [/^\/sales\/config(?:\/|$)/, L34],
  [/^\/feed\/packing(?:\/|$)/, L35],
  [/^\/herd-signals(?:\/|$)/, L36],
  [/^\/sales\/loads(?:\/|$)/, L37],
  [/^\/vaccination(?:\/|$)/, L38],
  [/^\/feed\/config(?:\/|$)/, L39],
  [/^\/counts\/herd(?:\/|$)/, L40],
  [/^\/counts\/sops(?:\/|$)/, L41],
  [/^\/work-board(?:\/|$)/, L42],
  [/^\/sales\/sold(?:\/|$)/, L43],
  [/^\/milk\/sops(?:\/|$)/, L44],
  [/^\/workflows(?:\/|$)/, L45],
  [/^\/feed\/sops(?:\/|$)/, L46],
  [/^\/approvals(?:\/|$)/, L47],
  [/^\/calendar(?:\/|$)/, L48],
  [/^\/routines(?:\/|$)/, L49],
  [/^\/weighing(?:\/|$)/, L50],
  [/^\/verify(?:\/|$)/, L51],
  [/^\/alerts(?:\/|$)/, L52],
  [/^\/people(?:\/|$)/, L53],
  [/^\/tasks(?:\/|$)/, L54],
  [/^\/leave(?:\/|$)/, L56],
];

export function routeSkeletonElement(pathname: string): ReactNode {
  for (const [pattern, Component] of ROUTE_SKELETONS) if (pattern.test(pathname)) return <Component />;
  return <GenericPageSkeleton />;
}

/** The route-shaped skeleton for the current pathname (the generic page skeleton when the route has none). */
export function RouteSkeleton() {
  const pathname = usePathname();
  return routeSkeletonElement(pathname ?? "/");
}
