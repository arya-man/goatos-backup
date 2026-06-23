import { FirebaseSessionBridge } from "@/components/auth/firebase-session-bridge";
import { MeshaShell } from "@/components/mesha-shell";

export function AdminShell({ children }: { children: React.ReactNode }) {
  return (
    <>
      <FirebaseSessionBridge />
      <MeshaShell>{children}</MeshaShell>
    </>
  );
}
