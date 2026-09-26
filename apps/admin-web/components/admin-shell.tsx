import { FirebaseSessionBridge } from "@/components/auth/firebase-session-bridge";
import { contractUnavailableCopy } from "@/components/admin-shell-unavailable";
import { MeshaShell, type ShellContract } from "@/components/mesha-shell";
import { ContractUnavailableTelemetry } from "@/components/observability/contract-unavailable-telemetry";
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
    const { kind, code, status, traceId } = contract.error;
    const copy = contractUnavailableCopy(kind);
    // The code is for the people who run the system, not the person at the screen: it goes to
    // the server log and to Faro, and the screen says what happened in farm words.
    console.error(JSON.stringify({ event: "admin_shell_contract_unavailable", kind, code, status, trace_id: traceId }));
    return (
      <>
        <FirebaseSessionBridge />
        <ContractUnavailableTelemetry kind={kind} code={code} status={status} traceId={traceId} />
        <main className="wrap" style={{ padding: 24 }}>
          <section className="card" style={{ padding: 20 }}>
            <h1>{copy.title}</h1>
            <p className="muted">{copy.body}</p>
            <div style={{ marginTop: 14 }}>
              {/* A plain reload of the page that failed: href="" is the current URL. */}
              <a className="btn primary" href={kind === "unauthorized" ? "/login" : ""}>
                {copy.retry}
              </a>
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
