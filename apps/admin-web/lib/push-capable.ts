/**
 * Browser feature-detect for Web Push, in one non-UI place: Notification + service worker +
 * PushManager on a secure context. False during SSR.
 */
export function isPushCapable(): boolean {
  if (typeof window === "undefined") return false;
  return (
    typeof window.Notification !== "undefined" &&
    typeof navigator.serviceWorker !== "undefined" &&
    typeof window.PushManager !== "undefined" &&
    window.isSecureContext === true
  );
}
