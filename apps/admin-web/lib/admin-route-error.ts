const SAFE_ERROR_REFERENCE = /^[A-Za-z0-9._:-]{1,80}$/;

export type NextRouteError = Error & { digest?: string };

// Next.js may attach an opaque digest to a Server Component failure. It is safe
// to show only that bounded identifier; the raw error message can contain
// server implementation details and must stay in telemetry/logs.
export function adminRouteErrorReference(error: NextRouteError): string | null {
  const digest = typeof error.digest === "string" ? error.digest.trim() : "";
  return SAFE_ERROR_REFERENCE.test(digest) ? digest : null;
}
