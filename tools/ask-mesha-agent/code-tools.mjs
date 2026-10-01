// Read-only codebase tools for the Gemini agent: the same reach the Claude Agent SDK's
// Read/Grep/Glob/Skill tools had (the shared goatos checkout + this chat's attachments), and
// nothing more. No shell, no writes. Every path is resolved through pathAllowed() (realpath, so
// symlinks can't climb out) and secret-looking files are refused even inside the checkout.
import fs from "node:fs";
import path from "node:path";
import { execFile } from "node:child_process";
import { pathAllowed } from "./lib.mjs";

export const READ_MAX_LINES = 2000;
export const LINE_MAX_CHARS = 2000;
export const TOOL_OUTPUT_MAX_CHARS = 40_000;
export const GLOB_MAX_FILES = 300;
const INLINE_MIME = { ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".pdf": "application/pdf" };
export const INLINE_MAX_BYTES = 7 * 1024 * 1024;

// Credential-shaped names (the image scrub removes these too; this is the runtime net).
const SECRET_RE = /(^|\/)(\.env(\..*)?|\.pgenv|\.npmrc|\.netrc|\.git-credentials|id_(rsa|ed25519|ecdsa)[^/]*|[^/]*\.(pem|key|p12|pfx|jks|keystore)|[^/]*(service[-_]?account|credentials?)[^/]*\.json|application_default_credentials\.json)$/i;
// The same credential shapes as ripgrep globs, so grep/glob never list or search them.
export const SECRET_GLOBS = [
  "**/.env", "**/.env.*", "**/.pgenv", "**/.npmrc", "**/.netrc", "**/.git-credentials", "**/id_rsa*", "**/id_ed25519*", "**/id_ecdsa*",
  "**/*.pem", "**/*.key", "**/*.p12", "**/*.pfx", "**/*.jks", "**/*.keystore",
  "**/*service-account*.json", "**/*service_account*.json", "**/*serviceaccount*.json", "**/*credential*.json", "**/application_default_credentials.json",
  "**/.git/**", "**/node_modules/**",
];
export function isSecretPath(p) {
  return SECRET_RE.test(String(p).replace(/\\/g, "/"));
}

export function inlineMime(p) {
  return INLINE_MIME[path.extname(String(p)).toLowerCase()] || null;
}

const clip = (s, max = TOOL_OUTPUT_MAX_CHARS) => (s.length > max ? s.slice(0, max) + `\n… (output clipped at ${max} chars; narrow the search)` : s);

// roots: directories readable besides the repo (this chat's attachment dirs).
export function createCodeTools({ repo, roots = [], rgBin = "rg", execImpl = execFile, signal } = {}) {
  const allRoots = [repo, ...roots];
  function resolveSafe(p, { mustExist = true } = {}) {
    const raw = String(p ?? "").trim() || ".";
    const abs = path.resolve(repo, raw);
    if (!pathAllowed(abs, repo, allRoots)) return { ok: false, out: "Ask Mesha can only read the goatos repo and this chat's attachments." };
    if (mustExist && !fs.existsSync(abs)) return { ok: false, out: `No such file or directory: ${raw}` };
    // Deny checks on the path as written AND its realpath (a symlink named notes.txt -> .env).
    let realAbs = abs;
    try { realAbs = fs.realpathSync(abs); } catch {}
    for (const p of [abs, realAbs]) {
      if (isSecretPath(p) || p.split(path.sep).includes(".git")) return { ok: false, out: "That file is not readable here." };
    }
    return { ok: true, abs: realAbs };
  }
  const rg = (args, cwd) => new Promise((resolve) => {
    execImpl(rgBin, ["--no-config", "--color=never", ...args], { cwd, timeout: 20_000, maxBuffer: 8 * 1024 * 1024, ...(signal ? { signal } : {}) }, (err, stdout, stderr) => {
      // rg exits 1 for "no matches": not an error.
      if (err && err.code !== 1) return resolve({ ok: false, out: String(stderr || err.message).slice(0, 2000) });
      resolve({ ok: true, out: String(stdout || "") });
    });
  });
  const rel = (abs) => (abs.startsWith(repo + path.sep) ? path.relative(repo, abs) : abs);

  async function read_file({ path: p, file_path, offset, limit } = {}) {
    const r = resolveSafe(p ?? file_path);
    if (!r.ok) return { text: r.out, isError: true };
    const st = fs.statSync(r.abs);
    if (st.isDirectory()) return list_dir({ path: r.abs });
    const mime = inlineMime(r.abs);
    if (mime) {
      if (st.size > INLINE_MAX_BYTES) return { text: `File is too large to view (${st.size} bytes).`, isError: true };
      return { text: `Attached ${path.basename(r.abs)} (${mime}) for you to look at.`, inline: { mimeType: mime, data: fs.readFileSync(r.abs).toString("base64") } };
    }
    const buf = fs.readFileSync(r.abs);
    if (buf.subarray(0, 8000).includes(0)) return { text: "Binary file; can't show it as text.", isError: true };
    const lines = buf.toString("utf8").split("\n");
    const start = Math.max(1, Number(offset) || 1);
    const n = Math.min(READ_MAX_LINES, Math.max(1, Number(limit) || READ_MAX_LINES));
    const slice = lines.slice(start - 1, start - 1 + n);
    const body = slice.map((l, i) => `${String(start + i).padStart(6)}\t${l.length > LINE_MAX_CHARS ? l.slice(0, LINE_MAX_CHARS) + "…" : l}`).join("\n");
    const more = start - 1 + n < lines.length ? `\n… (${lines.length} lines total; pass offset=${start + n} to continue)` : "";
    return { text: clip(body + more) || "(empty file)" };
  }

  async function grep({ pattern, path: p, glob, case_insensitive, output_mode = "files_with_matches", context, head_limit } = {}) {
    if (!pattern) return { text: "Give a pattern (regular expression).", isError: true };
    const r = resolveSafe(p || ".");
    if (!r.ok) return { text: r.out, isError: true };
    const args = [];
    if (case_insensitive) args.push("-i");
    if (glob) args.push("--glob", String(glob));
    for (const g of SECRET_GLOBS) args.push("--glob", `!${g}`);
    if (output_mode === "content") { args.push("-n"); if (context) args.push("-C", String(Math.min(10, Number(context) || 0))); }
    else if (output_mode === "count") args.push("-c");
    else args.push("-l");
    args.push("--max-columns", "400", "-e", String(pattern), "--", r.abs);
    const res = await rg(args, repo);
    if (!res.ok) return { text: res.out, isError: true };
    let lines = res.out.split("\n").filter(Boolean).map((l) => (l.startsWith(repo + path.sep) ? l.slice(repo.length + 1) : l))
      .filter((l) => !isSecretPath(l.split(":")[0]));
    const lim = Math.max(1, Math.min(1000, Number(head_limit) || 250));
    const total = lines.length;
    lines = lines.slice(0, lim);
    return { text: clip(lines.join("\n") + (total > lim ? `\n… (${total} results; showing ${lim})` : "")) || "No matches." };
  }

  async function glob({ pattern, path: p } = {}) {
    if (!pattern) return { text: "Give a glob pattern, e.g. backend/internal/**/weigh*.go", isError: true };
    const r = resolveSafe(p || ".");
    if (!r.ok) return { text: r.out, isError: true };
    const res = await rg(["--files", "--glob", String(pattern), ...SECRET_GLOBS.flatMap((g) => ["--glob", `!${g}`]), r.abs], repo);
    if (!res.ok) return { text: res.out, isError: true };
    const files = res.out.split("\n").filter(Boolean).filter((f) => !isSecretPath(f)).map((f) => rel(path.resolve(repo, f))).sort();
    const shown = files.slice(0, GLOB_MAX_FILES);
    return { text: shown.join("\n") + (files.length > shown.length ? `\n… (${files.length} files; narrow the pattern)` : "") || "No files matched." };
  }

  async function list_dir({ path: p } = {}) {
    const r = resolveSafe(p || ".");
    if (!r.ok) return { text: r.out, isError: true };
    const entries = fs.readdirSync(r.abs, { withFileTypes: true })
      .filter((e) => e.name !== ".git" && e.name !== "node_modules" && !isSecretPath(e.name))
      .map((e) => (e.isDirectory() ? `${e.name}/` : e.name)).sort();
    return { text: entries.slice(0, 500).join("\n") || "(empty directory)" };
  }

  // Skills: the SDK loaded SKILL.md on demand (Skill tool). Same here: list, then load by name.
  const SKILL_DIRS = [".claude/skills", ".agents/skills"];
  function skillNames() {
    const names = new Set();
    for (const d of SKILL_DIRS) {
      const dir = path.join(repo, d);
      if (!fs.existsSync(dir)) continue;
      for (const e of fs.readdirSync(dir, { withFileTypes: true })) if (e.isDirectory() && fs.existsSync(path.join(dir, e.name, "SKILL.md"))) names.add(e.name);
    }
    return [...names].sort();
  }
  async function get_skill({ name } = {}) {
    const names = skillNames();
    const n = String(name || "").trim();
    if (!n || n === "list") return { text: `Skills: ${names.join(", ")}` };
    if (!names.includes(n)) return { text: `Unknown skill ${n}. Skills: ${names.join(", ")}`, isError: true };
    for (const d of SKILL_DIRS) {
      const f = path.join(repo, d, n, "SKILL.md");
      if (fs.existsSync(f)) return { text: `# Skill ${n} (${d}/${n}/SKILL.md; its references/ files can be opened with read_file)\n\n` + clip(fs.readFileSync(f, "utf8")) };
    }
    return { text: `Skill ${n} not found.`, isError: true };
  }

  return { read_file, grep, glob, list_dir, get_skill, skillNames, resolveSafe };
}

// Gemini function declarations for the code tools (JSON Schema).
export const CODE_TOOL_DECLARATIONS = [
  {
    name: "read_file",
    description: "Read a file from the goatos codebase (current working directory, the live commit) or one of this chat's attachments. Returns numbered lines; use offset/limit for long files. Images/PDFs (screenshots) are shown to you directly.",
    parametersJsonSchema: { type: "object", properties: {
      path: { type: "string", description: "Repo-relative path (e.g. backend/internal/sales/app/service.go) or an attachment path from the question" },
      offset: { type: "integer", description: "1-based first line (default 1)" },
      limit: { type: "integer", description: "Number of lines (default and max 2000)" },
    }, required: ["path"] },
  },
  {
    name: "grep",
    description: "Search file contents in the goatos codebase with ripgrep (regular expression). Default output lists matching files; output_mode=content shows matching lines with line numbers.",
    parametersJsonSchema: { type: "object", properties: {
      pattern: { type: "string", description: "Regular expression" },
      path: { type: "string", description: "Directory or file to search (repo-relative, default the repo root)" },
      glob: { type: "string", description: "Only files matching this glob, e.g. *.go or backend/**/*.sql" },
      case_insensitive: { type: "boolean" },
      output_mode: { type: "string", enum: ["files_with_matches", "content", "count"] },
      context: { type: "integer", description: "Lines of context around each match (content mode, max 10)" },
      head_limit: { type: "integer", description: "Max result lines (default 250)" },
    }, required: ["pattern"] },
  },
  {
    name: "glob",
    description: "Find files by name pattern in the goatos codebase, e.g. **/*weigh*.go or apps/admin-web/**/ceo-ai*.tsx.",
    parametersJsonSchema: { type: "object", properties: {
      pattern: { type: "string" }, path: { type: "string", description: "Directory to search in (default repo root)" },
    }, required: ["pattern"] },
  },
  {
    name: "list_dir",
    description: "List one directory of the goatos codebase.",
    parametersJsonSchema: { type: "object", properties: { path: { type: "string" } } },
  },
  {
    name: "get_skill",
    description: "Load a repo skill's instructions (SKILL.md) by name, e.g. mesha-data-map. name='list' lists the skills. Use mesha-data-map for any data question the cheat-sheet doesn't fully cover.",
    parametersJsonSchema: { type: "object", properties: { name: { type: "string" } }, required: ["name"] },
  },
];
