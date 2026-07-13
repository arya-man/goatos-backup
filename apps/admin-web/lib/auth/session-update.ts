// Ordering for a session update (pure, dependency-injected so it is unit
// testable without Next.js request/response, a live Firebase exchange, or the
// backend audit endpoint).
//
// The security-critical invariant: the backend auth event (auth.sign_in /
// auth.session_refresh) records a SUCCESSFUL authentication. It must NOT be
// written for a request that is then rejected — a tampered pair that pairs
// account A's id token with account B's refresh token would otherwise leave a
// successful sign-in in the audit trail and immediately 401, corrupting the
// security/audit history. So the refresh-token binding (which detects the
// tampered pair) is resolved FIRST; only once the pair is trustworthy is the
// event recorded, and only once the event is recorded does the session commit.

import type { RefreshBindingDecision } from "./refresh-binding";

export type AuthEventResult = { ok: true } | { ok: false; status: number; error: string };

export type SessionUpdatePlan =
  | { outcome: "reject"; status: number; error: string; eventRecorded: false }
  | { outcome: "audit_failed"; status: number; error: string; eventRecorded: false }
  | { outcome: "commit"; binding: RefreshBindingDecision; eventRecorded: true };

export async function planSessionUpdate(deps: {
  resolveBinding: () => Promise<RefreshBindingDecision>;
  recordEvent: () => Promise<AuthEventResult>;
}): Promise<SessionUpdatePlan> {
  // 1. Bind the refresh token first — a rejected (tampered) pair fails here with
  //    NO audit event written.
  const binding = await deps.resolveBinding();
  if (binding.decision === "reject") {
    return { outcome: "reject", status: 401, error: "refresh_token_user_mismatch", eventRecorded: false };
  }

  // 2. Only a trustworthy pair records the successful authentication event.
  const audit = await deps.recordEvent();
  if (!audit.ok) {
    return { outcome: "audit_failed", status: audit.status, error: audit.error, eventRecorded: false };
  }

  // 3. Session commits (caller writes the id-token + refresh-token cookies).
  return { outcome: "commit", binding, eventRecorded: true };
}
