// Refresh-token binding decision (pure, dependency-injected so it is unit
// testable without Next.js request/response or a live Firebase exchange).
//
// The id-token cookie is verified (the session route audits it through the
// backend, which checks the Firebase signature). The long-lived refresh token is
// the DURABLE credential — after the ~1h id token lapses, SSR mints new id tokens
// from it for up to 14 days. An unbound refresh token lets a caller pair account
// A's (verified) id token with account B's refresh token: the session starts as A
// and silently becomes B once the id token expires. So the refresh token must be
// proven to belong to the same user before it is persisted.
//
// Decisions:
//   store  — exchange succeeded and the minted token's uid matches; persist the
//            rotated refresh token returned by the exchange (current + bound).
//   reject — exchange succeeded but the uid does NOT match; a tampered pair. The
//            caller must reject the whole request and set no cookies.
//   skip   — nothing to verify against, or the exchange could not run (e.g. web
//            API key absent). Fall back to an id-token-only session; never
//            persist an unverified refresh token.

export type ExchangedRefreshToken = {
  idToken: string;
  refreshToken: string;
};

export type RefreshBindingDecision =
  | { decision: "store"; refreshToken: string }
  | { decision: "reject" }
  | { decision: "skip" };

export type RefreshTokenExchange = (
  refreshToken: string,
) => Promise<ExchangedRefreshToken | null>;

export type UidFromToken = (token: string) => string | null;

export async function resolveBoundRefreshToken(
  idTokenUid: string | null,
  refreshToken: string,
  exchange: RefreshTokenExchange,
  uidFromToken: UidFromToken,
): Promise<RefreshBindingDecision> {
  const token = refreshToken.trim();
  if (!token || !idTokenUid) {
    return { decision: "skip" };
  }

  const exchanged = await exchange(token);
  if (!exchanged || !exchanged.idToken.trim() || !exchanged.refreshToken.trim()) {
    // The exchange endpoint is unavailable/failed; we cannot prove ownership.
    return { decision: "skip" };
  }

  const exchangedUid = uidFromToken(exchanged.idToken);
  if (!exchangedUid || exchangedUid !== idTokenUid) {
    return { decision: "reject" };
  }

  return { decision: "store", refreshToken: exchanged.refreshToken.trim() };
}
