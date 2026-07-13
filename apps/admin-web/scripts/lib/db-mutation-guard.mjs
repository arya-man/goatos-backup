// DRV-R3 local-DB mutation guard for the `dev:local` wrapper — the JS mirror of
// tools/dev/lib/db-mutation-guard.sh. Pure + importable so it is unit-tested (db-mutation-guard.test.mjs).
//
// Only a recognized auto-detected local docker container is TRUSTED for migrate/seed without asking.
// An inherited DATABASE_URL, or the no-docker 127.0.0.1:5433 fallback, is UNTRUSTED — a Cloud SQL Auth
// Proxy (or an unrelated Postgres) also listens on 127.0.0.1, so those require an explicit opt-in.

/**
 * @param {{ inheritedUrl?: string, dockerDetectedUrl?: string, fallbackUrl: string }} args
 * @returns {{ url: string, trusted: boolean, source: "inherited"|"docker"|"fallback" }}
 */
export function classifyLocalDatabaseUrl({ inheritedUrl, dockerDetectedUrl, fallbackUrl }) {
  if (typeof inheritedUrl === "string" && inheritedUrl !== "") {
    return { url: inheritedUrl, trusted: false, source: "inherited" };
  }
  if (typeof dockerDetectedUrl === "string" && dockerDetectedUrl !== "") {
    return { url: dockerDetectedUrl, trusted: true, source: "docker" };
  }
  return { url: fallbackUrl, trusted: false, source: "fallback" };
}

/**
 * Exits the process (fail-closed) when an UNTRUSTED database would be mutated without opt-in.
 * @param {boolean} trusted
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {boolean} true when mutation is allowed (used by tests; real callers exit on refusal)
 */
export function assertMutableLocalDb(trusted, env = process.env) {
  const optIn = env.GOATOS_ALLOW_DB_MUTATION === "1" || env.GOATOS_ALLOW_DB_MUTATION === "true";
  if (!trusted && !optIn) {
    console.error(
      "Refusing to migrate/seed: DATABASE_URL is not a verified disposable LOCAL database\n" +
        "(inherited from the environment, or the no-docker 127.0.0.1:5433 fallback — a Cloud SQL Auth\n" +
        "Proxy also listens there). Set GOATOS_ALLOW_DB_MUTATION=1 to opt in, or start a recognized local\n" +
        "docker Postgres container.",
    );
    process.exit(1);
  }
  return true;
}
