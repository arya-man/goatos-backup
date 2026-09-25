import { FirebaseSessionBridge } from "@/components/auth/firebase-session-bridge";
import { MeshaShell, type ShellContract } from "@/components/mesha-shell";
import { getAdminWebBootstrap, type AdminWebBootstrapResponse } from "@/lib/api/server";
import type { Park } from "@/lib/scope";

function parksFromContract(contract: AdminWebBootstrapResponse): Park[] {
  return contract.top_bar.park_selector.options.map((option) => ({
    id: option.key,
    code: option.label,
    name: option.title || option.label,
  }));
}

// The client shell only reads each page's href; the full page contracts (~1.2 MB) stay on the
// server so they are not serialized into every page's RSC/HTML payload.
function toShellContract(contract: AdminWebBootstrapResponse): ShellContract {
  return { ...contract, pages: contract.pages.map((page) => ({ href: page.href })) };
}

// Server component: business UI renders only after the backend-owned bootstrap contract succeeds.
// Top-bar park options are compiled into that contract from the backend locations source.
export async function AdminShell({ children }: { children: React.ReactNode }) {
  const contract = await getAdminWebBootstrap();
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
      <MeshaShell parks={parksFromContract(contract.data)} contract={toShellContract(contract.data)}>{children}</MeshaShell>
    </>
  );
}
