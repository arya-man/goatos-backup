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

export type AuthEventResult = { ok: true } | { ok: false; status: number; error: string; retryAfter?: string };

export type SessionUpdatePlan =
  | { outcome: "reject"; status: number; error: string; eventRecorded: false }
  | { outcome: "audit_failed"; status: number; error: string; retryAfter?: string; eventRecorded: false }
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
    return {
      outcome: "audit_failed",
      status: audit.status,
      error: audit.error,
      ...(audit.retryAfter ? { retryAfter: audit.retryAfter } : {}),
      eventRecorded: false,
    };
  }

  // 3. Session commits (caller writes the id-token + refresh-token cookies).
  return { outcome: "commit", binding, eventRecorded: true };
}

/**
 * Maps a failed backend /auth/session-events response onto the browser-facing result. 4xx
 * passes through; 503 (the auth database is busy, retryable) passes through WITH its Retry-After
 * so the browser can retry rather than treat it as a broken sign-in; any other 5xx is a 502.
 */
export function authEventFailure(
  status: number,
  code: string | null,
  retryAfter: string | null,
): Extract<AuthEventResult, { ok: false }> {
  const error = code ?? "auth_audit_failed";
  if (status === 503) {
    return { ok: false, status: 503, error, ...(retryAfter ? { retryAfter } : {}) };
  }
  return { ok: false, status: status >= 400 && status < 500 ? status : 502, error };
}
