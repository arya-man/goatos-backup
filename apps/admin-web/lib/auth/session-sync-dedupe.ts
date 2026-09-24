// Deduplicates POST /api/auth/session (-> backend POST /auth/session-events).
//
// Incident goatos-stg 2026-09-24: one admin user emitted ~12 `auth.sign_in` events in 6 minutes
// because the session bridge re-posted `auth.sign_in` with a FORCED token refresh on every page
// load and every onIdTokenChanged, on top of the explicit login's own sign_in. Every force refresh
// minted a new ID token, which also defeated the server's per-token read cache.
//
// Rules this module enforces:
// - `auth.sign_in` is recorded at most once per browser session per Firebase uid. The bridge still
//   records it once for a session restored from Firebase persistence (no explicit login), because
//   the backend claims pending CEO/CXO email grants only on sign_in. A grant created mid-session
//   is claimed at the next sign-in (new tab/browser session or re-login); the backend refuses to
//   claim on session_refresh by design (authaudit TestHandlerDoesNotClaimPendingEmailGrantOnRefresh).
// - A token that has already been synced (or is being synced) is never posted again.
// - Concurrent syncs of the same token share one request.

export type SessionSyncEventType = "auth.sign_in" | "auth.session_refresh";

export type SessionSyncStore = {
  get(key: string): string | null;
  set(key: string, value: string): void;
  remove(key: string): void;
};

const SIGN_IN_KEY_PREFIX = "goatos.admin.auth.sign_in_recorded:";
const SYNCED_TOKEN_KEY_PREFIX = "goatos.admin.auth.synced_token:";

// The JWT signature tail is unique per token; the full token is never stored.
function tokenFingerprint(idToken: string): string {
  return idToken.slice(-32);
}

export class SessionSyncDeduper {
  private readonly inflight = new Map<string, { eventType: SessionSyncEventType; promise: Promise<boolean> }>();
  private readonly store: SessionSyncStore;

  constructor(store: SessionSyncStore) {
    this.store = store;
  }

  /**
   * Explicit login. Always records sign_in for this token (the user just proved credentials),
   * sharing any in-flight sync of the same token.
   */
  signIn(uid: string, idToken: string, post: (eventType: SessionSyncEventType) => Promise<boolean>): Promise<boolean> {
    const pending = this.inflight.get(idToken);
    if (pending && pending.eventType !== "auth.sign_in") {
      // A background refresh of this token is in flight; the login still owes its own sign_in.
      return pending.promise
        .catch(() => false)
        .then(() => this.run(uid, idToken, "auth.sign_in", post));
    }
    return this.run(uid, idToken, "auth.sign_in", post);
  }

  /** Sign-out: the next login of this uid in this tab records auth.sign_in again. */
  forget(uid: string): void {
    this.remove(SIGN_IN_KEY_PREFIX + uid);
    this.remove(SYNCED_TOKEN_KEY_PREFIX + uid);
  }

  /**
   * Bridge / background sync. Skips a token already synced; otherwise records sign_in only when
   * this browser session has not recorded one for the uid, else a session_refresh.
   */
  bridge(uid: string, idToken: string, post: (eventType: SessionSyncEventType) => Promise<boolean>): Promise<boolean> {
    const pending = this.inflight.get(idToken);
    if (pending) return pending.promise;
    if (this.read(SYNCED_TOKEN_KEY_PREFIX + uid) === tokenFingerprint(idToken)) {
      return Promise.resolve(true);
    }
    const eventType: SessionSyncEventType = this.read(SIGN_IN_KEY_PREFIX + uid) ? "auth.session_refresh" : "auth.sign_in";
    return this.run(uid, idToken, eventType, post);
  }

  private run(
    uid: string,
    idToken: string,
    eventType: SessionSyncEventType,
    post: (eventType: SessionSyncEventType) => Promise<boolean>,
  ): Promise<boolean> {
    const pending = this.inflight.get(idToken);
    if (pending?.eventType === eventType) return pending.promise;
    const promise = post(eventType)
      .then((ok) => {
        if (ok) {
          this.write(SYNCED_TOKEN_KEY_PREFIX + uid, tokenFingerprint(idToken));
          if (eventType === "auth.sign_in") this.write(SIGN_IN_KEY_PREFIX + uid, "1");
        }
        return ok;
      })
      .finally(() => {
        if (this.inflight.get(idToken)?.promise === promise) this.inflight.delete(idToken);
      });
    this.inflight.set(idToken, { eventType, promise });
    return promise;
  }

  private read(key: string): string | null {
    try {
      return this.store.get(key);
    } catch {
      return null;
    }
  }

  private remove(key: string): void {
    try {
      this.store.remove(key);
    } catch {
      // Storage unavailable: nothing was persisted to clear.
    }
  }

  private write(key: string, value: string): void {
    try {
      this.store.set(key, value);
    } catch {
      // Storage unavailable (private mode): in-flight dedupe still applies.
    }
  }
}

export function memorySessionSyncStore(): SessionSyncStore {
  const values = new Map<string, string>();
  return {
    get: (key) => values.get(key) ?? null,
    set: (key, value) => void values.set(key, value),
    remove: (key) => void values.delete(key),
  };
}

export function browserSessionSyncStore(): SessionSyncStore {
  if (typeof window === "undefined" || !window.sessionStorage) return memorySessionSyncStore();
  const storage = window.sessionStorage;
  return {
    get: (key) => storage.getItem(key),
    set: (key, value) => storage.setItem(key, value),
    remove: (key) => storage.removeItem(key),
  };
}
