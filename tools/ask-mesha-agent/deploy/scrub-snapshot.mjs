// Strips anything secret-looking from the /repo code snapshot baked into the Ask Mesha image.
// Runs at image build (Dockerfile) AFTER the .dockerignore filter, as a second net: files whose
// NAME or CONTENT looks like a credential are deleted (not failed on), so a new kind of secret
// file never reaches the agent. Prints removed paths (never contents).
// usage: node scrub-snapshot.mjs <dir> [--dry-run]
import fs from "node:fs";
import path from "node:path";

export const NAME_PATTERNS = [
  /^\.env(\..*)?$/i, /^\.envrc$/i, /\.env$/i, /\.tfvars\.json$|^secrets?\.auto\.tfvars$/i, /^kubeconfig$/i, /\.(pem|key|p12|pfx|jks|keystore|kdbx|ovpn|ppk|asc|gpg)$/i,
  /^id_(rsa|dsa|ecdsa|ed25519)$/i,
  // secret/credential in the NAME only for data files (code/docs/terraform that merely mention secrets stay)
  /(^|[-_.])(secret|secrets|credential|credentials)([-_.][^.]*)?\.(json|ya?ml|txt|env|ini|cfg|conf|properties|csv)$/i,
  /service[-_]?account.*\.json$/i, /-sa\.json$/i, /^\.(pgenv|pgpass|npmrc|netrc|pypirc|dockercfg|git-credentials)$/i,
  /^google-services\.json$/i, /^GoogleService-Info\.plist$/i, /\.tfstate(\..*)?$/i, /^\.htpasswd$/i,
];
// Keep example/template files; they document shape, not values.
const ALLOW_NAME = /(\.example|\.sample|\.template)$|^\.env\.example$/i;

export const CONTENT_PATTERNS = [
  /-----BEGIN (RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY( BLOCK)?-----/,
  /"private_key"\s*:\s*"-----BEGIN/,           // GCP service-account JSON
  /\bsk-ant-(api|admin|oat)\d{2}-[A-Za-z0-9_-]{20,}/, // Anthropic keys
  /\bsk-(proj-)?[A-Za-z0-9]{32,}\b/,             // OpenAI-style keys
  /\bAKIA[0-9A-Z]{16}\b/,                         // AWS access key id
  /\bAIza[0-9A-Za-z_-]{35}\b/,                    // Google API key
  /\bgh[pousr]_[A-Za-z0-9]{36,}\b/,               // GitHub tokens
  /\bgithub_pat_[A-Za-z0-9_]{60,}\b/,             // GitHub fine-grained PATs
  /\bglpat-[A-Za-z0-9_-]{20,}\b/,                 // GitLab PATs
  /\bGOCSPX-[A-Za-z0-9_-]{24,}\b/,                // Google OAuth client secret
  /\b[rs]k_live_[A-Za-z0-9]{20,}\b/,              // Stripe live keys
  /\bnpm_[A-Za-z0-9]{36}\b/,                      // npm tokens
  /\bhooks\.slack\.com\/services\/T[A-Z0-9]+\/B[A-Z0-9]+\/[A-Za-z0-9]{20,}/, // Slack webhooks
  /\bxox[abprs]-[A-Za-z0-9-]{10,}/,               // Slack tokens
  // Real DSN with a password; <placeholder>, loopback and example hosts are docs/tests, not secrets.
  /\bpostgres(ql)?:\/\/[^\s:@/'"]+:[^\s@/'"$<>{}]{6,}@(?!localhost|127\.0\.0\.1|\[::1\]|db[:/]|postgres:|[^\s/@]*example\.(com|org|net)\b)/i,
];

const MAX_SCAN = 2 * 1024 * 1024;

export function nameIsSecret(base) {
  return !ALLOW_NAME.test(base) && NAME_PATTERNS.some((re) => re.test(base));
}
export function contentIsSecret(text) {
  return CONTENT_PATTERNS.some((re) => re.test(text));
}

export function scrub(dir, { dryRun = false } = {}) {
  const removed = [];
  const root = path.resolve(dir);
  const walk = (d) => {
    for (const ent of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, ent.name);
      if (ent.isSymbolicLink()) {
        // Keep links that stay inside the snapshot (e.g. .claude/skills -> .agents/skills); drop links that escape it.
        const target = path.resolve(path.dirname(p), fs.readlinkSync(p));
        if (target === root || target.startsWith(root + path.sep)) continue;
        removed.push([p, "symlink outside snapshot"]); if (!dryRun) fs.unlinkSync(p); continue;
      }
      if (ent.isDirectory()) { walk(p); continue; }
      if (!ent.isFile()) continue;
      let why = nameIsSecret(ent.name) ? "name" : null;
      if (!why) {
        // Large files are scanned too (their first MAX_SCAN bytes): skipping them was a false negative.
        const fd = fs.openSync(p, "r");
        const buf = Buffer.alloc(Math.min(fs.fstatSync(fd).size, MAX_SCAN));
        try { fs.readSync(fd, buf, 0, buf.length, 0); } finally { fs.closeSync(fd); }
        if (!buf.subarray(0, 8000).includes(0) && contentIsSecret(buf.toString("utf8"))) why = "content";
      }
      if (why) { removed.push([p, why]); if (!dryRun) fs.rmSync(p, { force: true }); }
    }
  };
  walk(root);
  return removed;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const dir = process.argv[2];
  if (!dir) { console.error("usage: scrub-snapshot.mjs <dir> [--dry-run]"); process.exit(2); }
  const removed = scrub(dir, { dryRun: process.argv.includes("--dry-run") });
  for (const [p, why] of removed) console.log(`scrub-snapshot: removed ${path.relative(dir, p)} (${why})`);
  console.log(`scrub-snapshot: ${removed.length} file(s) removed`);
}
