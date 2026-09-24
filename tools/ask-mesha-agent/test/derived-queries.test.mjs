import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const gen = join(here, '..', 'gen-data-map.mjs');
const manifest = join(here, '..', '..', '..', '.agents/skills/mesha-data-map/references/derived-queries.json');
const env = (p) => ({ ...process.env, GEN_DATA_MAP_DERIVED: p, PGHOST: '', PGDATABASE: '', PGSERVICE: '' });

test('committed manifest covers ADG, cost per kg gain and feed stock, and is in sync', () => {
  const m = JSON.parse(readFileSync(manifest, 'utf8'));
  const qs = m.queries.map((q) => q.query).join(' ');
  for (const f of ['adg-by-park.sql', 'cost-per-kg-gain.sql', 'feed-stock-days-left.sql']) assert.match(qs, new RegExp(f));
  const r = spawnSync('node', [gen, '--check'], { encoding: 'utf8', env: env(manifest) });
  assert.doesNotMatch(r.stderr, /app logic behind/);
});

test('--check fails with re-derive instructions when a source file hash drifts', () => {
  const m = JSON.parse(readFileSync(manifest, 'utf8'));
  m.queries[0].derived_from[0].sha256 = '0'.repeat(64);
  const p = join(mkdtempSync(join(tmpdir(), 'dq-')), 'derived-queries.json');
  writeFileSync(p, JSON.stringify(m));
  const r = spawnSync('node', [gen, '--check'], { encoding: 'utf8', env: env(p) });
  assert.equal(r.status, 1);
  assert.match(r.stderr, new RegExp(m.queries[0].derived_from[0].path.replace(/[.]/g, '\\.') + ' \\(source changed\\)'));
  assert.match(r.stderr, /Re-derive/);
  assert.match(r.stderr, /--rehash-derived/);
});

test('pen resolver is drift-guarded: a new migration touching pen tables fails with the pens.sql re-validate text', () => {
  const m = JSON.parse(readFileSync(manifest, 'utf8'));
  const pens = m.queries.find((q) => q.query.endsWith('/pens.sql'));
  assert.ok(pens, 'pens.sql in manifest');
  const dir = pens.derived_from.find((s) => s.match);
  assert.ok(dir && /goat_shed_partitions/.test(dir.match), 'migrations directory watched with a match regex');
  for (const f of ['oploc.go', 'fcr.go', 'shed_partition_resolve.go']) assert.ok(pens.derived_from.some((s) => s.path.endsWith(f)), f);
  dir.sha256 = '0'.repeat(64);
  const p = join(mkdtempSync(join(tmpdir(), 'dq-')), 'derived-queries.json');
  writeFileSync(p, JSON.stringify(m));
  const r = spawnSync('node', [gen, '--check'], { encoding: 'utf8', env: env(p) });
  assert.equal(r.status, 1);
  assert.match(r.stderr, /new\/changed matching migration/);
  assert.match(r.stderr, /re-validate pens\.sql for the new pen model \(see docs\/agent-rules\/ask-mesha\.md Pens\)/);
});
