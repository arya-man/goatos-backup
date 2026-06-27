import { FirebaseSessionBridge } from "@/components/auth/firebase-session-bridge";
import { MeshaShell } from "@/components/mesha-shell";
import { getScopeParks } from "@/lib/api/scope-parks";
import { getAdminWebBootstrap } from "@/lib/api/server";

// Server component: fetch the parks list once for the top-bar scope control so the shell can render human
// park labels from the locations API while the URL/API carry the backend-safe location UUID.
export async function AdminShell({ children }: { children: React.ReactNode }) {
  const [parks, contract] = await Promise.all([getScopeParks(), getAdminWebBootstrap()]);
  if (!contract.ok) {
    return (
      <>
        <FirebaseSessionBridge />
        <main className="wrap" style={{ padding: 24 }}>
          <section className="card">
            <h1>Admin-web contract unavailable</h1>
            <p className="muted">
              The backend-owned UI contract could not be loaded, so admin-web is not rendering local fallback IA.
              Resolve the API/session/tenant error and reload.
            </p>
            <div className="metagrid" style={{ marginTop: 14 }}>
              <div>
                <div className="k">Error</div>
                <div className="v">{contract.error.code ?? contract.error.kind}</div>
              </div>
              <div>
                <div className="k">Detail</div>
                <div className="v">{contract.error.message}</div>
              </div>
            </div>
          </section>
        </main>
      </>
    );
  }
  return (
    <>
      <FirebaseSessionBridge />
      <MeshaShell parks={parks} contract={contract.data}>{children}</MeshaShell>
    </>
  );
}
