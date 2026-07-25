// Standalone stdlib-only module so the CEO-AI answer-quality eval harness runs
// independently of the backend module and needs no third-party dependencies.
// The Postgres oracle is executed by shelling out to `psql` (libpq), so no SQL
// driver has to be vendored. See tools/ceo-ai/eval/README.md.
module github.com/vgoats/goatos/tools/ceo-ai/eval

go 1.23
