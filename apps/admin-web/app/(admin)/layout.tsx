import { Suspense } from "react";
import { AdminShell } from "@/components/admin-shell";
import { ObservabilityErrorBoundary } from "@/components/observability/error-boundary";
import { ShellSkeleton } from "@/components/shell-skeleton";
import { RouteSkeleton } from "@/components/route-skeleton";

// The shell-shaped skeleton (top bar + sidebar column) shows while the bootstrap contract is
// fetched. Its page column is the SAME skeleton the route's loading.tsx renders (route-aware by
// pathname), so a direct load is one continuous skeleton: shell chrome fills in around a page
// shape that never changes until the content arrives. Routes without a loading.tsx get the
// generic page skeleton.
function AdminShellFallback() {
  return (
    <ShellSkeleton>
      <RouteSkeleton />
    </ShellSkeleton>
  );
}

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return (
    <ObservabilityErrorBoundary>
      <Suspense fallback={<AdminShellFallback />}>
        <AdminShell>{children}</AdminShell>
      </Suspense>
    </ObservabilityErrorBoundary>
  );
}
