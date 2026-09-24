// Pure grading helpers for the Ask Mesha accuracy regression (eval/run.mjs). No I/O here, so
// test/eval-grade.test.mjs can pin every rule.

// Words a CEO-facing answer must not contain (docs/agent-rules/ask-mesha.md: no SQL/table talk).
export const CODE_TALK = [
  "\\bsql\\b", "\\bceo_ai\\.", "\\bpublic\\.[a-z_]+", "\\bschema\\b", "\\bcolumns?\\b", "\\brun_sql\\b", "\\brun_reference\\b",
  "\\b[a-z_]+\\.sql\\b", "\\b(table|view) name", "\\bjson_agg\\b", "\\blifecycle_status\\b",
];
const ZERO_WORDS = /\b(no|none|zero|nil|nothing|not any|0|doesn'?t have any|don'?t have any|no (sales|records?|data))\b/i;

// ---- dates (IST business days) ------------------------------------------------
const IST_MS = 330 * 60_000;
const ymd = (d) => d.toISOString().slice(0, 10);
export function dateMacros(now = new Date()) {
  const ist = new Date(now.getTime() + IST_MS);
  const y = ist.getUTCFullYear(), m = ist.getUTCMonth(), day = ist.getUTCDate();
  const today = new Date(Date.UTC(y, m, day));
  const dow = (today.getUTCDay() + 6) % 7; // Monday = 0
  const week_start = new Date(today.getTime() - dow * 864e5);
  return {
    today: ymd(today),
    month_start: ymd(new Date(Date.UTC(y, m, 1))),
    prev_month_start: ymd(new Date(Date.UTC(y, m - 1, 1))),
    prev_month_end: ymd(new Date(Date.UTC(y, m, 0))),
    week_start: ymd(week_start),
    week_end: ymd(new Date(week_start.getTime() + 6 * 864e5)),
    days_ago_21: ymd(new Date(today.getTime() - 21 * 864e5)),
    days_ago_30: ymd(new Date(today.getTime() - 30 * 864e5)),
  };
}
export function fillMacros(value, macros) {
  if (typeof value === "string") return value.replace(/\{\{([a-z_0-9]+)\}\}/g, (all, k) => (k in macros ? macros[k] : all));
  if (Array.isArray(value)) return value.map((v) => fillMacros(v, macros));
  if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, fillMacros(v, macros)]));
  return value;
}

// ---- numbers in an answer ------------------------------------------------------
// "4,50,175" / "1,562" / "28.2" / "Rs 4.5 lakh" / "37.4L" / "1.2 crore" / "12k". Scaled forms are added
// alongside the raw number, so "4.5 lakh" yields both 4.5 and 450000.
export function extractNumbers(text) {
  const out = [];
  const re = /(?<![\w.])(-?\d[\d,]*(?:\.\d+)?)(\s*(lakhs?|lacs?|l\b|crores?|cr\b|k\b|thousand))?/gi;
  for (const m of String(text || "").matchAll(re)) {
    const n = Number(m[1].replace(/,/g, ""));
    if (!Number.isFinite(n)) continue;
    out.push(n);
    const unit = (m[3] || "").toLowerCase();
    if (/^(lakh|lac|l$)/.test(unit)) out.push(n * 1e5);
    else if (/^(crore|cr)/.test(unit)) out.push(n * 1e7);
    else if (unit === "k" || unit === "thousand") out.push(n * 1e3);
  }
  return out;
}
export function numberPresent(text, value, { tol = 0, tol_pct = 0 } = {}) {
  const v = Number(value);
  if (!Number.isFinite(v)) return false;
  const slack = Math.max(Number(tol) || 0, (Math.abs(v) * (Number(tol_pct) || 0)) / 100, 1e-9);
  return extractNumbers(text).some((n) => Math.abs(n - v) <= slack || Math.abs(Math.abs(n) - Math.abs(v)) <= slack);
}

// DD/MM/YYYY, YYYY-MM-DD, "24 Sep", "24th September", "Sep 24".
const MONTHS = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];
export function datePresent(text, value) {
  const s = String(value || "");
  let d, m, y;
  let r = /^(\d{2})\/(\d{2})\/(\d{4})$/.exec(s);
  if (r) [, d, m, y] = r; else if ((r = /^(\d{4})-(\d{2})-(\d{2})/.exec(s))) [, y, m, d] = r; else return false;
  const t = String(text || "").toLowerCase();
  const dd = String(Number(d)), mon = MONTHS[Number(m) - 1];
  return t.includes(`${d}/${m}/${y}`) || t.includes(`${dd}/${Number(m)}/${y}`) || t.includes(`${y}-${m}-${d}`) || t.includes(`${d}/${m}`) ||
    new RegExp(`\\b0?${dd}(st|nd|rd|th)?\\s+${mon}`).test(t) || new RegExp(`\\b${mon}[a-z]*\\.?\\s+0?${dd}\\b`).test(t);
}

// ---- truth rows ------------------------------------------------------------------
const norm = (x) => String(x ?? "").trim().toLowerCase();
export function pickRow(rows, row) {
  if (!row) return rows[0];
  return rows.find((r) => Object.entries(row).every(([k, v]) => norm(r[k]) === norm(v)));
}
const truthy = (v) => v === true || v === "t" || v === "true" || (typeof v === "number" && v > 0);

// Expand expect[] against truth rows -> [{label, value, tol, tol_pct, kind, zero_words}] (value undefined = missing).
export function expectedValues(item, rows) {
  const out = [];
  for (const e of item.expect || []) {
    if (e.each_row) {
      for (const r of rows) out.push({ ...e, label: `${e.label} ${r[e.each_row] ?? ""}`.trim(), value: r[e.col] });
      if (!rows.length) out.push({ ...e, value: undefined });
    } else {
      const r = pickRow(rows, e.row);
      out.push({ ...e, value: r ? r[e.col] : undefined });
    }
  }
  return out;
}

// Grade one answer. rows = truth rows (array; [] for items without truth).
export function gradeAnswer(item, answer, rows = [], { seconds = null, maxSeconds = 90 } = {}) {
  const fails = [];
  const checks = [];
  const text = String(answer || "");
  if (!text.trim()) return { pass: false, reasons: ["empty answer"], checks };
  for (const ev of expectedValues(item, rows)) {
    if (ev.value === undefined) { fails.push(`truth has no ${ev.label} (row missing)`); continue; }
    if (ev.value === null) { checks.push({ label: ev.label, truth: null, ok: true, note: "truth NULL (not checked)" }); continue; }
    let ok;
    if (ev.kind === "date") ok = datePresent(text, ev.value);
    else {
      ok = numberPresent(text, ev.value, ev);
      if (!ok && ev.zero_words && Number(ev.value) === 0) ok = ZERO_WORDS.test(text);
    }
    checks.push({ label: ev.label, truth: ev.value, ok });
    if (!ok) fails.push(`missing ${ev.label}=${ev.value}${ev.tol ? ` ±${ev.tol}` : ev.tol_pct ? ` ±${ev.tol_pct}%` : ""}`);
  }
  for (const mc of item.mention_cols || []) {
    const r = pickRow(rows, mc.row);
    const v = r?.[mc.col];
    if (v == null || v === "") continue;
    if (!text.toLowerCase().includes(String(v).toLowerCase())) fails.push(`must name "${v}"`);
  }
  for (const mi of item.mention_if || []) {
    const r = pickRow(rows, mi.row);
    if (r && truthy(r[mi.col]) && !new RegExp(mi.regex, "i").test(text)) fails.push(`must flag ${mi.col} (/${mi.regex}/)`);
  }
  for (const re of item.must_mention || []) if (!new RegExp(re, "i").test(text)) fails.push(`must mention /${re}/`);
  const banned = [...(item.must_not_mention || []), ...(item.allow_code_words ? [] : CODE_TALK)];
  for (const re of banned) {
    const m = text.match(new RegExp(re, "i"));
    if (m) fails.push(`must not say "${m[0].slice(0, 40)}"`);
  }
  if (seconds != null && seconds > maxSeconds) fails.push(`slow: ${seconds}s > ${maxSeconds}s`);
  return { pass: fails.length === 0, reasons: fails, checks };
}

// ---- UI API extraction -------------------------------------------------------------
// path grammar: "a.b[field=val].c", "a[field~substr].c", "sum:items[].x", "min:items[].x", "count:items[]".
export function extractPath(obj, spec) {
  let agg = null;
  let p = String(spec);
  const m = /^(sum|min|max|count):(.*)$/.exec(p);
  if (m) { agg = m[1]; p = m[2]; }
  let cur = [obj];
  let many = false;
  for (const seg of p.split(".").filter(Boolean)) {
    const s = /^([^[\]]+)(?:\[([^\]]*)\])?$/.exec(seg);
    if (!s) return undefined;
    const [, key, filt] = s;
    cur = cur.map((o) => (o == null ? undefined : o[key]));
    if (filt === undefined) continue;
    const arrs = cur.flatMap((a) => (Array.isArray(a) ? a : []));
    if (filt === "") { cur = arrs; many = true; continue; }
    const f = /^([^=~]+)([=~])(.*)$/.exec(filt);
    if (!f) return undefined;
    const [, fk, op, fv] = f;
    const hit = arrs.find((x) => (op === "=" ? norm(x?.[fk]) === norm(fv) : norm(x?.[fk]).includes(norm(fv))));
    cur = [hit];
  }
  const vals = cur.filter((v) => v !== undefined);
  if (agg === "count") return vals.length;
  const nums = vals.map(Number).filter(Number.isFinite);
  if (agg === "sum") return nums.reduce((a, b) => a + b, 0);
  if (agg === "min") return nums.length ? Math.min(...nums) : undefined;
  if (agg === "max") return nums.length ? Math.max(...nums) : undefined;
  return many ? vals : vals[0];
}

// 3-way verdict per expect label: chat vs SQL vs UI.
export function threeWay({ label, truth, chatOk, ui, tol = 0, tol_pct = 0, answer = "" }) {
  if (ui === undefined || ui === null) return { label, truth, ui: ui ?? null, verdict: chatOk ? "ok (no UI value)" : "chat differs (chat bug)" };
  const slack = Math.max(Number(tol) || 0, (Math.abs(Number(truth)) * (Number(tol_pct) || 0)) / 100, 1e-9);
  const uiMatches = Math.abs(Number(ui) - Number(truth)) <= slack;
  if (uiMatches) return { label, truth, ui, verdict: chatOk ? "ok" : "chat differs (chat bug)" };
  const chatMatchesUi = numberPresent(answer, ui, { tol, tol_pct });
  return { label, truth, ui, verdict: chatMatchesUi ? "UI differs from SQL (possible UI bug); chat matches UI" : "UI differs from SQL (possible UI bug)" };
}
