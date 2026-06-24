import { FirebaseSessionBridge } from "@/components/auth/firebase-session-bridge";
import { MeshaShell } from "@/components/mesha-shell";
import { getScopeParks } from "@/lib/api/scope-parks";

// Server component: fetch the parks list once for the top-bar scope control so the shell can render human
// park labels (CBE) while the URL/API carry the backend-safe location UUID.
export async function AdminShell({ children }: { children: React.ReactNode }) {
  const parks = await getScopeParks();
  return (
    <>
      <FirebaseSessionBridge />
      <MeshaShell parks={parks}>{children}</MeshaShell>
    </>
  );
}
