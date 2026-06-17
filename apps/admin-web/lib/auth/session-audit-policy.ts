export type AuthSessionEventType = "auth.sign_in" | "auth.session_refresh" | "auth.sign_out";

export function allowUnauditedSessionRefresh(
  eventType: AuthSessionEventType,
  auditStatus: number,
  hasExistingSessionCookie: boolean,
): boolean {
  return eventType === "auth.session_refresh" && auditStatus >= 500 && hasExistingSessionCookie;
}
