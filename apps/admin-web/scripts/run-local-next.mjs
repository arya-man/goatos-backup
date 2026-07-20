import { execFileSync, spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import net from "node:net";
import path from "node:path";
import { assertMutableLocalDb, classifyLocalDatabaseUrl } from "./lib/db-mutation-guard.mjs";
import { assertOriginMainLocalStack } from "./lib/origin-main-local-stack-guard.mjs";

const mode = process.argv[2];
const host = "127.0.0.1";
const port = 3300;
const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const backendDir = path.join(repoRoot, "backend");

const defaultTenantId = "00000000-0000-4000-8000-000000000001";
const defaultUserId = "90000000-0000-4000-8000-000000000101";
const defaultRole = "ceo_internal";
const defaultApiBaseUrl = "http://127.0.0.1:8080";
// DRV-R3a: classify the local DB target — only a recognized auto-detected docker container is trusted;
// an inherited DATABASE_URL OR the no-docker 127.0.0.1:5433 fallback is untrusted (needs an opt-in).
const databaseClassification = classifyLocalDatabaseUrl({
  inheritedUrl: process.env.GOATOS_ALLOW_CUSTOM_LOCAL_STACK_DB === "1" ? process.env.DATABASE_URL : "",
  dockerDetectedUrl: detectDockerDatabaseUrl(),
  fallbackUrl: "postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable",
});
const defaultDatabaseUrl = databaseClassification.url;
const defaultHS256Secret = "goatos-local-dev-secret-32-bytes-min";

if (mode !== "dev" && mode !== "start") {
  console.error("Usage: npm run dev:local|start:local");
  process.exit(2);
}

assertOriginMainLocalStack(repoRoot, "admin-web");
assertNotTempCheckout();
await assertPortFree(host, port);
const childEnv = await prepareLocalEnvironment();

const nextArgs = [mode, "-H", host, "-p", String(port)];
if (mode === "dev") {
  // Local macOS dev stacks must be reproducible across machines; Turbopack can fail when
  // the native binding is unavailable, leaving Chrome pointed at an older respawned server.
  nextArgs.push("--webpack");
}

const child = spawn("next", nextArgs, {
  stdio: "inherit",
  env: { ...childEnv, HOSTNAME: host, PORT: String(port) },
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 0);
});

async function assertPortFree(hostname, targetPort) {
  const busy = await new Promise((resolve, reject) => {
    const socket = net.createConnection({ host: hostname, port: targetPort });
    socket.once("connect", () => {
      socket.destroy();
      resolve(true);
    });
    socket.once("error", (error) => {
      socket.destroy();
      if (error.code === "ECONNREFUSED") {
        resolve(false);
        return;
      }
      reject(error);
    });
    socket.setTimeout(1000, () => {
      socket.destroy();
      resolve(false);
    });
  });

  if (!busy) return;

  console.error(`Port ${targetPort} on ${hostname} is already in use. Stop that process or choose a different terminal.`);
  try {
    const owner = execFileSync("lsof", ["-nP", `-iTCP:${targetPort}`, "-sTCP:LISTEN"], { encoding: "utf8" });
    console.error(owner.trim());
  } catch {
    console.error("Could not inspect owner with lsof.");
  }
  process.exit(1);
}

async function prepareLocalEnvironment() {
  const localEnv = {
    ...process.env,
    GOATOS_ENV: process.env.GOATOS_ENV || "local",
    GOATOS_AUTH_MODE: process.env.GOATOS_AUTH_MODE || "bearer",
    GOATOS_AUTH_ISSUER: process.env.GOATOS_AUTH_ISSUER || "goatos-local",
    GOATOS_AUTH_AUDIENCE: process.env.GOATOS_AUTH_AUDIENCE || "goatos-api",
    GOATOS_AUTH_HS256_SECRET: process.env.GOATOS_AUTH_HS256_SECRET || defaultHS256Secret,
    GOATOS_AUTH_MAX_TOKEN_TTL: process.env.GOATOS_AUTH_MAX_TOKEN_TTL || "24h",
    DATABASE_URL: localStackDatabaseUrl(),
    GOATOS_API_BASE_URL: process.env.GOATOS_API_BASE_URL || defaultApiBaseUrl,
    GOATOS_TENANT_ID: process.env.GOATOS_TENANT_ID || process.env.GOATOS_LOCAL_TENANT_ID || defaultTenantId,
  };

  const autoAuth = process.env.GOATOS_LOCAL_DEV_AUTO_AUTH;
  if (autoAuth === "0" || autoAuth === "false") {
    console.log("local dev auto-auth disabled; using existing GOATOS_BEARER_TOKEN.");
    return localEnv;
  }

  const tenantId = process.env.GOATOS_LOCAL_TENANT_ID || localEnv.GOATOS_TENANT_ID;
  const userId = process.env.GOATOS_LOCAL_USER_ID || defaultUserId;
  const role = process.env.GOATOS_LOCAL_ROLE || defaultRole;
  const ttl = process.env.GOATOS_LOCAL_TOKEN_TTL || "12h";

  // DRV-R3 single-owner prep: when the stack orchestrator (run-local-stack.sh / supervised) already
  // migrated + seeded + ran closeout, it hands off GOATOS_LOCAL_DB_PREPARED=1 so this wrapper does NOT
  // repeat those heavier DB mutations. The local dev grant is intentionally still ensured below:
  // it is idempotent, and a disposable reseed without this row makes /admin-web/bootstrap fail with
  // permission_denied even though the dev token is valid.
  const alreadyPrepared =
    process.env.GOATOS_LOCAL_DB_PREPARED === "1" || process.env.GOATOS_LOCAL_DB_PREPARED === "true";
  if (alreadyPrepared) {
    console.log("Local database already prepared by the stack orchestrator; skipping migrate/seed/closeout.");
    ensureLocalDevGrant(localEnv, tenantId, userId, role);
  } else {
    // DRV-R3a fail-closed guard: refuse to migrate/seed unless the target is a TRUSTED local docker DB.
    // An inherited DATABASE_URL OR the no-docker 127.0.0.1:5433 fallback (a Cloud SQL Auth Proxy also
    // listens there) is untrusted and needs an explicit GOATOS_ALLOW_DB_MUTATION=1 opt-in.
    assertMutableLocalDb(databaseClassification.trusted);

    console.log("Applying Goat OS local migrations before admin-web start.");
    runGo(["run", "./cmd/migrate", "-timeout=10m"], localEnv);

    ensureLocalDevGrant(localEnv, tenantId, userId, role);
    runSeedCloseoutIfPresent(localEnv);
  }

  const bearerToken = runGo(["run", "./cmd/mint-dev-token", "-tenant-id", tenantId, "-user-id", userId, "-ttl", ttl], localEnv).trim();
  if (bearerToken === "") {
    console.error("mint-dev-token returned an empty token.");
    process.exit(1);
  }

  const envWithToken = {
    ...localEnv,
    GOATOS_TENANT_ID: tenantId,
    GOATOS_BEARER_TOKEN: bearerToken,
  };

  await validateBackendAuth(envWithToken);
  console.log(`Local admin token refreshed. Open http://${host}:${port}/`);
  return envWithToken;
}

function localStackDatabaseUrl() {
  const explicit = process.env.GOATOS_LOCAL_STACK_DATABASE_URL;
  if (explicit) return explicit;
  if (process.env.DATABASE_URL && process.env.DATABASE_URL !== defaultDatabaseUrl && process.env.GOATOS_ALLOW_CUSTOM_LOCAL_STACK_DB !== "1") {
    console.error("Refusing to inherit DATABASE_URL for the local admin-web stack.");
    console.error(`Inherited: ${process.env.DATABASE_URL}`);
    console.error(`Expected:  ${defaultDatabaseUrl}`);
    console.error("Set GOATOS_LOCAL_STACK_DATABASE_URL for the local stack, or GOATOS_ALLOW_CUSTOM_LOCAL_STACK_DB=1 for an explicit experiment.");
    process.exit(2);
  }
  return defaultDatabaseUrl;
}

function ensureLocalDevGrant(localEnv, tenantId, userId, role) {
  console.log(`Preparing local dev auth for tenant ${tenantId}, user ${userId}, role ${role}.`);
  runGo(["run", "./cmd/seed-dev-grant", "-tenant-id", tenantId, "-user-id", userId, "-role", role], localEnv);
}

function detectDockerDatabaseUrl() {
  try {
    const output = execFileSync("docker", ["ps", "--format", "{{.Names}}|{{.Ports}}"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
    const candidates = [];
    for (const line of output.split("\n")) {
      const [name = "", ports = ""] = line.split("|");
      if (!isLocalAppPostgresContainer(name)) continue;
      const match = ports.match(/127\.0\.0\.1:(\d+)->5432\/tcp/);
      if (match) candidates.push({ name, port: match[1] });
    }

    if (candidates.length > 1) {
      console.error("Multiple Goat OS local app Postgres containers are running; refusing to guess DATABASE_URL.");
      for (const candidate of candidates) {
        console.error(`${candidate.name}:${candidate.port}`);
      }
      console.error("Stop the extra container or set DATABASE_URL explicitly.");
      process.exit(1);
    }
    if (candidates.length === 1) {
      return `postgres://postgres:goatos@127.0.0.1:${candidates[0].port}/goatos?sslmode=disable`;
    }
  } catch {
    return "";
  }
  return "";
}

function isLocalAppPostgresContainer(name) {
  return name === "goatos-local-current";
}

function assertNotTempCheckout() {
  if (process.env.GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK === "1") return;

  let topLevel = repoRoot;
  try {
    topLevel = execFileSync("git", ["-C", repoRoot, "rev-parse", "--show-toplevel"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    // Non-git local bundles still get the path check below.
  }

  if (topLevel.startsWith("/tmp/") || topLevel.startsWith("/private/tmp/") || topLevel.startsWith("/var/folders/")) {
    console.error(`Refusing to start Mesha admin-web from a temporary worktree:\n  ${topLevel}`);
    console.error("");
    console.error("Start admin-web from the canonical checkout, or set");
    console.error("GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK=1 for an explicit throwaway experiment.");
    process.exit(2);
  }
}

function runSeedCloseoutIfPresent(env) {
  const seedCloseout = path.join(repoRoot, "tools/dev/seed-closeout.sh");
  try {
    execFileSync("test", ["-f", seedCloseout], { stdio: "ignore" });
  } catch {
    return;
  }

  try {
    console.log(`Running Goat OS local seed closeout for tenant ${env.GOATOS_TENANT_ID}.`);
    execFileSync("bash", [seedCloseout], {
      cwd: repoRoot,
      env,
      stdio: "inherit",
    });
  } catch (error) {
    process.exit(error.status || 1);
  }
}

function runGo(args, env) {
  try {
    const output = execFileSync("go", args, {
      cwd: backendDir,
      env,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    if (args.includes("./cmd/seed-dev-grant") && output.trim() !== "") {
      console.log(output.trim());
    }
    return output;
  } catch (error) {
    if (error.stdout?.toString().trim()) {
      console.error(error.stdout.toString().trim());
    }
    if (error.stderr?.toString().trim()) {
      console.error(error.stderr.toString().trim());
    }
    process.exit(error.status || 1);
  }
}

async function validateBackendAuth(env) {
  const apiBaseUrl = env.GOATOS_API_BASE_URL.replace(/\/+$/, "");
  const ready = await fetchWithTimeout(`${apiBaseUrl}/readyz`, {});
  if (ready.kind === "network") {
    console.warn(`Backend not reachable at ${apiBaseUrl}; Next will start, but live data will wait for the API.`);
    return;
  }
  if (ready.status < 200 || ready.status >= 300) {
    console.error(`Backend at ${apiBaseUrl} is not ready: HTTP ${ready.status}.`);
    process.exit(1);
  }

  const authCheck = await fetchWithTimeout(`${apiBaseUrl}/goats/search?limit=1`, {
    headers: { Authorization: `Bearer ${env.GOATOS_BEARER_TOKEN}` },
  });
  if (authCheck.kind === "network") {
    console.error(`Backend disappeared while validating auth at ${apiBaseUrl}: ${authCheck.error}`);
    process.exit(1);
  }
  if (authCheck.status === 401) {
    console.error("Fresh local token was rejected with 401. Restart the backend with the same GOATOS_AUTH_* env or run `make dev-local`.");
    process.exit(1);
  }
  if (authCheck.status === 403) {
    console.error("Fresh local token was valid but unauthorized. Check the local dev grant and tenant id, or run `make dev-local`.");
    process.exit(1);
  }
  if (authCheck.status < 200 || authCheck.status >= 300) {
    const body = await authCheck.text();
    console.error(`Backend auth check failed: HTTP ${authCheck.status} ${body}`);
    process.exit(1);
  }
}

async function fetchWithTimeout(url, init) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 1500);
  try {
    const response = await fetch(url, { ...init, signal: controller.signal });
    return response;
  } catch (error) {
    return { kind: "network", error: error?.message || String(error) };
  } finally {
    clearTimeout(timeout);
  }
}
