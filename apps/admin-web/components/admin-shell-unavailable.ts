// The shell's emergency screen copy, shown when the page contract itself could not be loaded.
//
// Deliberately LOCAL (documented exception in context/frontend/admin-web-backend-ui-contract.md):
// the contract request is what failed, so its copy cannot come from it. It is still farm words --
// the screen used to print the raw code `backend_down` under "Admin-web contract unavailable"
// (Sales E2E, 2026-09-26). The error's kind, code and trace id go to telemetry only
// (ContractUnavailableTelemetry), never onto the screen.

export type ContractUnavailableCopy = {
  title: string;
  body: string;
  retry: string;
};

const RETRY = "Try again";

export function contractUnavailableCopy(kind: string): ContractUnavailableCopy {
  switch (kind) {
    case "backend_down":
      return {
        title: "Mesha is not reachable right now",
        body: "The farm system did not answer. Check your connection, wait a minute and try again.",
        retry: RETRY,
      };
    case "unauthorized":
      return {
        title: "Please sign in again",
        body: "Your sign-in has ended. Sign in again to open the dashboard.",
        retry: "Sign in",
      };
    case "permission_denied":
    case "tenant_scope_mismatch":
      return {
        title: "This account cannot open the dashboard",
        body: "Your access does not include the dashboard yet. Ask an admin to check it on People.",
        retry: RETRY,
      };
    default:
      return {
        title: "The dashboard could not load",
        body: "Something went wrong while opening your workspace. Wait a minute and try again.",
        retry: RETRY,
      };
  }
}
