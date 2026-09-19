#!/usr/bin/env node

// check-ceo-ai-schema-cards.mjs — every governed reporting view the leadership
// assistant may read must have a repo-owned schema card (plan v3 D1.1).
//
// The card (backend/internal/ceoai/reporting/schema_cards.go) is what the
// planner prompt renders, what sqlguard.ValidateWindow consults for the view's
// date column, and what the one-shot SQL repair hands back to the model. A view
// created without a card is invisible to the planner, un-windowable and
// un-repairable: the model would be drafting against a relation it was never
// told about. This guard fails closed on exactly that.
//
// Check:
//   for every `CREATE OR REPLACE VIEW ceo_ai.<name>` (case-insensitive,
//   whitespace-tolerant) in backend/migrations/postgres/*.sql, a card with
//   `Name: "<name>"` must exist in schema_cards.go. A view that is later
//   dropped (`DROP VIEW ceo_ai.<name>` in a NEWER migration's Up section) is
//   not required — but that is only relevant once such a migration exists;
//   today every ceo_ai view created is live. Down-section DROPs are ignored.
//
// Not checked here (the Go tests do it): column-level parity with
// information_schema (Postgres-gated TestSchemaCardsMatchInformationSchema),
// banned-keyword column names, tenant_id presence.
//
// Modes:
//   (default)     scan the repo, fail on any view without a card.
//   --self-test   run the built-in adversarial fixtures against the pure
//                 checker (no repo scan).
//
// Deterministic, offline: pure text parsing. No network, no build, no DB.

import { readFileSync, readdirSync } from "node:fs";
import { resolve, join } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const MIGRATIONS_DIR = "backend/migrations/postgres";
const CARDS_FILE = "backend/internal/ceoai/reporting/schema_cards.go";

const CREATE_VIEW_RE = /create\s+or\s+replace\s+view\s+ceo_ai\.([a-z0-9_]+)/gi;
const CARD_NAME_RE = /^\s*Name:\s*"([a-z0-9_]+)"\s*,\s*$/gm;

// viewsFromMigrations returns { name -> first file that creates it } from the
// given { fileName -> sql text } map, ignoring everything after a
// `-- +goose Down` marker (a Down section re-creates the old shape and never
// introduces a live view).
export function viewsFromMigrations(files) {
  const out = new Map();
  const names = Object.keys(files).sort();
  for (const file of names) {
    let text = files[file];
    const down = text.search(/^\s*--\s*\+goose\s+Down\b/im);
    if (down >= 0) text = text.slice(0, down);
    for (const m of text.matchAll(CREATE_VIEW_RE)) {
      const name = m[1].toLowerCase();
      if (!out.has(name)) out.set(name, file);
    }
  }
  return out;
}

// cardsFromSource returns the set of card Names declared in schema_cards.go.
export function cardsFromSource(goSource) {
  const out = new Set();
  for (const m of goSource.matchAll(CARD_NAME_RE)) out.add(m[1].toLowerCase());
  return out;
}

// check returns the list of views (sorted) that have no card.
export function check(files, goSource) {
  const views = viewsFromMigrations(files);
  const cards = cardsFromSource(goSource);
  const missing = [];
  for (const [name, file] of views) {
    if (!cards.has(name)) missing.push({ name, file });
  }
  missing.sort((a, b) => a.name.localeCompare(b.name));
  return { missing, viewCount: views.size, cardCount: cards.size };
}

function loadRepo() {
  const dir = join(repo, MIGRATIONS_DIR);
  const files = {};
  for (const f of readdirSync(dir)) {
    if (!f.endsWith(".sql")) continue;
    files[f] = readFileSync(join(dir, f), "utf8");
  }
  const goSource = readFileSync(join(repo, CARDS_FILE), "utf8");
  return { files, goSource };
}

function selfTest() {
  const good = `
package reporting
var schemaCards = []SchemaCard{
	{
		Name:                "animal_current_scope",
		Purpose:             "x",
	},
	{
		Name: "mortality_base",
	},
}`;
  const cases = [
    {
      name: "every created view has a card",
      files: {
        "000001_base.sql": "CREATE OR REPLACE VIEW ceo_ai.animal_current_scope AS SELECT 1;\nCREATE OR REPLACE VIEW ceo_ai.mortality_base AS SELECT 1;",
      },
      go: good,
      wantMissing: [],
    },
    {
      name: "a view without a card fails",
      files: {
        "000001_base.sql": "CREATE OR REPLACE VIEW ceo_ai.animal_current_scope AS SELECT 1;",
        "000400_new.sql": "-- +goose Up\nCREATE OR REPLACE VIEW ceo_ai.sales_pipeline AS SELECT 1;\n-- +goose Down\nDROP VIEW ceo_ai.sales_pipeline;",
      },
      go: good,
      wantMissing: ["sales_pipeline"],
    },
    {
      name: "case and whitespace variants are still views",
      files: {
        "000001_base.sql": "create   or\n  replace VIEW CEO_AI.Weighing_Capture_Activity AS SELECT 1;",
      },
      go: good,
      wantMissing: ["weighing_capture_activity"],
    },
    {
      name: "a Down-section recreate does not require a card",
      files: {
        "000001_base.sql": "CREATE OR REPLACE VIEW ceo_ai.mortality_base AS SELECT 1;",
        "000401_x.sql": "-- +goose Up\nALTER TABLE goats ADD COLUMN y int;\n-- +goose Down\nCREATE OR REPLACE VIEW ceo_ai.legacy_only AS SELECT 1;",
      },
      go: good,
      wantMissing: [],
    },
    {
      name: "a card name inside a comment does not count",
      files: {
        "000001_base.sql": "CREATE OR REPLACE VIEW ceo_ai.feed_adherence AS SELECT 1;",
      },
      go: good + '\n// Name: "feed_adherence",\n',
      wantMissing: ["feed_adherence"],
    },
    {
      name: "a card name as a non-Name field does not count",
      files: {
        "000001_base.sql": "CREATE OR REPLACE VIEW ceo_ai.feed_adherence AS SELECT 1;",
      },
      go: good + '\n\tPurpose: "feed_adherence",\n',
      wantMissing: ["feed_adherence"],
    },
    {
      name: "a non-ceo_ai view is out of scope",
      files: {
        "000001_base.sql": "CREATE OR REPLACE VIEW public.some_view AS SELECT 1;",
      },
      go: good,
      wantMissing: [],
    },
  ];
  let failed = 0;
  for (const c of cases) {
    const got = check(c.files, c.go).missing.map((m) => m.name);
    const ok = JSON.stringify(got) === JSON.stringify(c.wantMissing);
    if (!ok) {
      failed += 1;
      console.error(`self-test FAIL: ${c.name}: got ${JSON.stringify(got)} want ${JSON.stringify(c.wantMissing)}`);
    }
  }
  if (failed > 0) {
    console.error(`ceo-ai-schema-card-guard self-test: ${failed} fixture(s) failed`);
    process.exit(1);
  }
  console.log(`ceo-ai-schema-card-guard self-test: ${cases.length} fixtures pass`);
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }
  const { files, goSource } = loadRepo();
  const { missing, viewCount, cardCount } = check(files, goSource);
  if (viewCount === 0) {
    console.error("ceo-ai-schema-card-guard: found no CREATE OR REPLACE VIEW ceo_ai.* in migrations; the scan is broken");
    process.exit(1);
  }
  if (missing.length > 0) {
    console.error("ceo-ai-schema-card-guard: every ceo_ai.* view needs a schema card (backend/internal/ceoai/reporting/schema_cards.go):\n");
    for (const m of missing) console.error(`  ceo_ai.${m.name}  (created in ${MIGRATIONS_DIR}/${m.file})`);
    console.error("\nAdd a SchemaCard with that Name (purpose, grain, date column, columns+types, route) — see docs/ceo-ai/schema-cards.md.");
    process.exit(1);
  }
  console.log(`ceo-ai-schema-card-guard: ${viewCount} ceo_ai views, ${cardCount} cards, every view has a card`);
}

main();
