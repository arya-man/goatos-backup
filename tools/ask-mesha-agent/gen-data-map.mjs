#!/usr/bin/env node
// Generates .agents/skills/mesha-data-map/references/views.generated.md:
// every ceo_ai object the current DB user can SELECT, with purpose/grain/date
// column (from backend/internal/ceoai/reporting/schema_cards.go), live columns
// and one example query. Deterministic output.
//
//   node tools/ask-mesha-agent/gen-data-map.mjs          # write
//   node tools/ask-mesha-agent/gen-data-map.mjs --check  # exit 1 if stale
//   node tools/ask-mesha-agent/gen-data-map.mjs --rehash-derived  # after re-deriving a reference query
//     --check also verifies references/derived-queries.json: every hand-copied query
//     (ADG, cost per kg gain, feed stock days left) lists the app source files it was
//     derived from + a sha256 of each; any change fails until the query is re-derived.
//     with DB env: byte-for-byte compare against a fresh render.
//     without DB env: only the object SET (schema_cards.go + migrations vs the
//     objects listed in the file) is checked; column-level check is skipped.
//
// DB access: standard PG* env (PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE)
// and the `psql` CLI. Without PG env it falls back to schema_cards.go +
// migrations only and says so in the header. Never writes to the DB.
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, existsSync, readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// GEN_DATA_MAP_ROOT lets refresh-data-map.sh point the generator at another checkout.
const ROOT = process.env.GEN_DATA_MAP_ROOT ? resolve(process.env.GEN_DATA_MAP_ROOT) : resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
const CARDS = join(ROOT, 'backend/internal/ceoai/reporting/schema_cards.go');
const MIGR = join(ROOT, 'backend/migrations/postgres');
const OUT = join(ROOT, '.agents/skills/mesha-data-map/references/views.generated.md');
const check = process.argv.includes('--check');
const DERIVED = process.env.GEN_DATA_MAP_DERIVED ? resolve(process.env.GEN_DATA_MAP_DERIVED) : join(ROOT, '.agents/skills/mesha-data-map/references/derived-queries.json');

// --- derived-query drift ----------------------------------------------------
const fileSha = (rel) => { const f = join(ROOT, rel); return existsSync(f) ? createHash('sha256').update(readFileSync(f)).digest('hex') : 'MISSING'; };
function derivedDrift(manifest, sha = fileSha) {
  const out = [];
  for (const q of manifest.queries) {
    if (!existsSync(join(ROOT, q.query))) out.push({ query: q.query, path: q.query, why: 'reference query file missing' });
    for (const src of q.derived_from) {
      const now = sha(src.path);
      if (now !== src.sha256) out.push({ query: q.query, path: src.path, why: now === 'MISSING' ? 'source file missing (moved/renamed?)' : 'source changed' });
    }
  }
  return out;
}
if (process.argv.includes('--rehash-derived')) {
  const m = JSON.parse(readFileSync(DERIVED, 'utf8'));
  for (const q of m.queries) for (const src of q.derived_from) src.sha256 = fileSha(src.path);
  writeFileSync(DERIVED, JSON.stringify(m, null, 2) + '\n');
  process.stdout.write(`gen-data-map: rehashed ${DERIVED}\n`);
  process.exit(0);
}
if (check) {
  const drift = derivedDrift(JSON.parse(readFileSync(DERIVED, 'utf8')));
  if (drift.length) {
    process.stderr.write('gen-data-map: app logic behind a hand-copied data-map query changed:\n' +
      drift.map((d) => `  - ${d.query}: ${d.path} (${d.why})\n`).join('') +
      'Re-derive each listed query from its source (diff the source since the manifest hash), re-verify its numbers against the app,\n' +
      'update the query + SKILL.md example numbers, then run: node tools/ask-mesha-agent/gen-data-map.mjs --rehash-derived\n');
    process.exitCode = 1;
  } else {
    process.stdout.write(`gen-data-map: derived queries in sync with app sources (${DERIVED})\n`);
  }
}

function sourceSha() {
  try {
    return execFileSync('git', ['-C', ROOT, 'log', '-1', '--format=%H', '--',
      'backend/internal/ceoai/reporting/schema_cards.go', 'backend/migrations/postgres'],
    { encoding: 'utf8' }).trim() || 'unknown';
  } catch { return 'unknown'; }
}

// --- schema cards -----------------------------------------------------------
function parseCards() {
  const src = readFileSync(CARDS, 'utf8');
  const body = src.slice(src.indexOf('var schemaCards'));
  const cards = new Map();
  for (const block of body.split(/\n\t\{\n/).slice(1)) {
    const str = (k) => (block.match(new RegExp(`\\b${k}:\\s*"((?:[^"\\\\]|\\\\.)*)"`)) || [])[1] || '';
    const name = str('Name');
    if (!name) continue;
    const list = (k) => {
      const m = block.match(new RegExp(`\\b${k}:\\s*\\[\\]string\\{([^}]*)\\}`));
      return m ? [...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1]) : [];
    };
    const typeMap = { uuidT: 'uuid', textT: 'text', dateT: 'date', tstzT: 'timestamptz', bigT: 'bigint', intT: 'integer', numT: 'numeric', dblT: 'double', boolT: 'boolean', jsonbT: 'jsonb' };
    const columns = [...block.matchAll(/col\("([^"]+)",\s*(\w+)\)/g)].map((m) => ({ name: m[1], type: typeMap[m[2]] || m[2] }));
    cards.set(name, {
      purpose: str('Purpose'), grain: str('Grain'), dateColumn: str('DateColumn'), parkColumn: str('ParkColumn'),
      groupBy: list('GroupByColumns'), neverAverage: list('NeverAverage'), route: str('Route'),
      aggregateOnly: /AggregateOnly:\s*true/.test(block), columns,
    });
  }
  return cards;
}

// --- live DB ----------------------------------------------------------------
function psql(sql) {
  return execFileSync('psql', ['-X', '-A', '-t', '-F', '\t', '-v', 'ON_ERROR_STOP=1', '-c', sql],
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env, PGOPTIONS: '-c default_transaction_read_only=on' } });
}
function liveObjects() {
  if (!process.env.PGHOST && !process.env.PGDATABASE && !process.env.PGSERVICE) return null;
  let out;
  try {
    out = psql(`SELECT c.table_schema, c.table_name, t.table_type, c.column_name, c.data_type, c.ordinal_position
      FROM information_schema.columns c JOIN information_schema.tables t USING (table_schema, table_name)
      WHERE c.table_schema = 'ceo_ai'
        AND CASE WHEN has_schema_privilege(c.table_schema, 'USAGE') THEN has_table_privilege(quote_ident(c.table_schema)||'.'||quote_ident(c.table_name), 'SELECT') ELSE false END
      ORDER BY 1, 2, 6`);
  } catch (e) {
    process.stderr.write(`gen-data-map: psql failed, falling back to repo sources: ${String(e.stderr || e.message).split('\n')[0]}\n`);
    return null;
  }
  const objs = new Map();
  for (const line of out.split('\n').filter(Boolean)) {
    const [schema, name, kind, colName, type] = line.split('\t');
    const key = `${schema}.${name}`;
    if (!objs.has(key)) objs.set(key, { schema, name, kind: kind === 'VIEW' ? 'view' : 'table', columns: [] });
    objs.get(key).columns.push({ name: colName, type: shortType(type) });
  }
  return objs;
}
function shortType(t) {
  return ({ 'timestamp with time zone': 'timestamptz', 'double precision': 'double', 'character varying': 'text' })[t] || t;
}
function repoObjects(cards) {
  const names = new Set();
  for (const f of readdirSync(MIGR).filter((f) => f.endsWith('.sql')).sort()) {
    for (const m of readFileSync(join(MIGR, f), 'utf8').matchAll(/CREATE\s+(?:OR\s+REPLACE\s+)?VIEW\s+ceo_ai\.(\w+)/gi)) names.add(m[1]);
  }
  for (const n of cards.keys()) names.add(n);
  const objs = new Map();
  for (const n of names) objs.set(`ceo_ai.${n}`, { schema: 'ceo_ai', name: n, kind: 'view', columns: cards.get(n)?.columns || [] });
  return objs;
}

// --- rendering --------------------------------------------------------------
function guessDate(cols) {
  const pref = ['business_date', 'business_day', 'feed_day', 'event_date', 'planned_business_date', 'due_business_day'];
  for (const p of pref) if (cols.some((c) => c.name === p)) return p;
  return cols.find((c) => c.type === 'date')?.name || '';
}
function exampleQuery(o, card) {
  const cols = o.columns.map((c) => c.name);
  const date = card?.dateColumn || '';
  const park = card?.parkColumn || (cols.includes('park_label') ? 'park_label' : '');
  const fq = `${o.schema}.${o.name}`;
  if (o.name === 'vaccine_label_map') return `SELECT family_prefix, vaccine_label FROM ${fq} ORDER BY 1;`;
  if (date) {
    const dims = [date, park].filter(Boolean);
    return `SELECT ${dims.join(', ')}, count(*) AS n\nFROM ${fq}\nWHERE ${date} >= (now() AT TIME ZONE 'Asia/Kolkata')::date - 30\nGROUP BY ${dims.map((_, i) => i + 1).join(', ')}\nORDER BY 1 DESC${park ? ', 2' : ''}\nLIMIT 50;`;
  }
  if (park) return `SELECT ${park}, count(*) AS n\nFROM ${fq}\nGROUP BY 1\nORDER BY 1;`;
  return `SELECT * FROM ${fq} LIMIT 20;`;
}
function render(objs, cards, live) {
  const L = [];
  L.push('<!-- GENERATED by tools/ask-mesha-agent/gen-data-map.mjs. Do not edit; rerun the generator. -->');
  L.push(`<!-- source: ${live ? 'live DB (information_schema, objects SELECTable by current user) + schema_cards.go' : 'FALLBACK: schema_cards.go + migrations only (no DB env); columns may lag the live DB'} @ ${sourceSha()} -->`);
  L.push('');
  L.push('# ceo_ai views: full reference');
  L.push('');
  L.push('Every object below lives in schema `ceo_ai`, is tenant-scoped via `tenant_id` (stg has one tenant), and');
  L.push('is read-only. Business dates are Asia/Kolkata (IST) calendar days. "shed" in column names = **pen** on screen.');
  L.push('Routing and gotchas: see ../SKILL.md.');
  L.push('');
  L.push('| object | grain | key date column |');
  L.push('|---|---|---|');
  const keys = [...objs.keys()].sort();
  for (const k of keys) {
    const o = objs.get(k); const c = cards.get(o.name);
    L.push(`| [${k}](#${k.replace(/\./g, '').toLowerCase()}) | ${c?.grain || '(no card)'} | ${c ? (c.dateColumn || 'none (current state)') : guessDate(o.columns) || 'none'} |`);
  }
  for (const k of keys) {
    const o = objs.get(k); const c = cards.get(o.name);
    L.push('', `## ${k}`, '');
    L.push(`- **Kind:** ${o.kind}`);
    L.push(`- **Purpose:** ${c?.purpose || (o.name === 'vaccine_label_map' ? 'Lookup: vaccine family prefix -> human vaccine label.' : '(no schema card; infer from columns)')}`);
    L.push(`- **Grain:** ${c?.grain || '(unknown)'}`);
    L.push(`- **Key date column:** ${c ? (c.dateColumn ? `\`${c.dateColumn}\`` : 'none (current state; answer "as of now")') : (guessDate(o.columns) ? `\`${guessDate(o.columns)}\` (guessed)` : 'none')}`);
    if (c?.parkColumn) L.push(`- **Park column:** \`${c.parkColumn}\``);
    if (c?.groupBy?.length) L.push(`- **Group by:** ${c.groupBy.map((x) => `\`${x}\``).join(', ')}`);
    if (c?.neverAverage?.length) L.push(`- **Never re-average/sum:** ${c.neverAverage.map((x) => `\`${x}\``).join(', ')}`);
    if (c?.aggregateOnly) L.push('- **Per-entity base view:** aggregate (count/sum GROUP BY) rather than dumping rows.');
    if (c?.route) L.push(`- **Admin-web drill-down:** \`${c.route}\``);
    L.push(`- **Columns:** ${o.columns.map((x) => `\`${x.name}\` ${x.type}`).join(', ') || '(unknown)'}`);
    L.push('', '```sql', exampleQuery(o, c), '```');
  }
  return L.join('\n') + '\n';
}

const cards = parseCards();
const live = liveObjects();
const objs = live || repoObjects(cards);
const text = render(objs, cards, !!live);
if (check) {
  const cur = existsSync(OUT) ? readFileSync(OUT, 'utf8') : '';
  if (live) {
    if (cur !== text) { process.stderr.write(`gen-data-map: ${OUT} is stale; run node tools/ask-mesha-agent/gen-data-map.mjs\n`); process.exit(1); }
    process.stdout.write(`gen-data-map: up to date (${objs.size} objects, live DB)\n`);
  } else {
    const listed = new Set([...cur.matchAll(/^## (ceo_ai\.\w+)$/gm)].map((m) => m[1]));
    const expected = new Set(objs.keys());
    const missing = [...expected].filter((k) => !listed.has(k)).sort();
    // The live DB may expose extra ceo_ai objects the repo can't see (e.g. lookups
    // without a CREATE VIEW), so extras are reported but not fatal.
    const extra = [...listed].filter((k) => !expected.has(k)).sort();
    if (!cur || missing.length) {
      process.stderr.write(`gen-data-map: ${OUT} is stale; missing objects: ${missing.join(', ') || '(file absent)'}\n` +
        'rerun with DB env: node tools/ask-mesha-agent/gen-data-map.mjs\n');
      process.exit(1);
    }
    process.stdout.write(`gen-data-map: object set consistent (${expected.size} repo objects listed${extra.length ? `; ${extra.length} DB-only: ${extra.join(', ')}` : ''}); ` +
      'column-level check SKIPPED (no DB env)\n');
  }
} else {
  writeFileSync(OUT, text);
  process.stdout.write(`gen-data-map: wrote ${OUT} (${objs.size} objects, ${live ? 'live DB' : 'repo fallback'})\n`);
}
