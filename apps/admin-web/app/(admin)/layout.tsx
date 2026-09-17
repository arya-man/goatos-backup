import { Suspense } from "react";
import { AdminShell } from "@/components/admin-shell";
import { ObservabilityErrorBoundary } from "@/components/observability/error-boundary";

function AdminShellFallback() {
  return (
    <main className="wrap" style={{ padding: 24 }}>
      <section className="card" aria-busy="true">
        <div className="bd">
          <div className="skel" style={{ width: 170, height: 14, marginBottom: 12 }} />
          <div className="skel" style={{ width: 260, height: 30, marginBottom: 10 }} />
          <div className="skel" style={{ width: "100%", maxWidth: 680, height: 18 }} />
        </div>
      </section>
    </main>
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
