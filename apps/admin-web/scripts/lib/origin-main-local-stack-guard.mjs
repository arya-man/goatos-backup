import { execFileSync } from "node:child_process";
import path from "node:path";

export function assertOriginMainLocalStack(repoRoot, serviceName, options = {}) {
  if (process.env.GOATOS_ALLOW_STALE_LOCAL_STACK === "1") return;
  const port = Number(options.port ?? process.env.PORT ?? 3000);
  if (!isSharedAdminWebPort(port)) return;

  const root = path.resolve(repoRoot);
  const remoteUrl = git(root, ["remote", "get-url", "origin"]);
  if (!remoteUrl.includes("github.com/vgoats/goatos")) {
    fail(serviceName, root, `origin is ${remoteUrl || "(missing)"}, expected github.com/vgoats/goatos`);
  }

  if (process.env.GOATOS_ORIGIN_MAIN_PREVERIFIED !== "1") {
    try {
      execFileSync("git", ["-C", root, "fetch", "--quiet", "origin", "main"], { stdio: "ignore" });
    } catch (error) {
      fail(serviceName, root, `could not fetch origin/main: ${error.message}`);
    }
  }

  const head = git(root, ["rev-parse", "HEAD"]);
  const originMain = git(root, ["rev-parse", "refs/remotes/origin/main"]);
  if (!head || !originMain || head !== originMain) {
    fail(serviceName, root, `HEAD ${short(head)} is not origin/main ${short(originMain)}`);
  }
  const dirty = git(root, ["status", "--porcelain", "--untracked-files=no"]);
  if (dirty) {
    fail(serviceName, root, "tracked files are modified; local stack must serve a clean origin/main checkout");
  }
}

export function isSharedAdminWebPort(port) {
  return Number(port) === 3300;
}

function git(root, args) {
  try {
    return execFileSync("git", ["-C", root, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
  } catch {
    return "";
  }
}

function fail(serviceName, root, reason) {
  console.error(`${serviceName} local stack guard blocked startup.`);
  console.error(`Reason: ${reason}`);
  console.error(`Checkout: ${root}`);
  console.error("");
  console.error("Local FE/BE/DB must run from exact origin/main so agents do not serve stale worktrees.");
  console.error("Update/restart from a fresh origin/main worktree, or set GOATOS_ALLOW_STALE_LOCAL_STACK=1 only for an explicit throwaway experiment.");
  process.exit(2);
}

function short(sha) {
  return sha ? sha.slice(0, 12) : "(missing)";
}
