import { AdminShell } from "@/components/admin-shell";
import { ObservabilityErrorBoundary } from "@/components/observability/error-boundary";

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return (
    <ObservabilityErrorBoundary>
      <AdminShell>{children}</AdminShell>
    </ObservabilityErrorBoundary>
  );
}
