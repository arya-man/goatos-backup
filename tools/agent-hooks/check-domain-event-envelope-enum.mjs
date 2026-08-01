#!/usr/bin/env node
/**
 * Every domain event a module emits must be in the envelope's event_type enum.
 *
 * The envelope schema validates each outbox message before it is published. An event whose type is
 * absent from the enum does not fail at compile time, at test time, or in any guard -- it is
 * accepted by the writer, stored, and then rejected by the relay as
 * `failed | invalid_event_envelope`. The write succeeds, the state change lands, and the entire
 * downstream leg (consumers, notifications, projections) simply never happens. Nothing surfaces
 * unless someone reads the outbox table by hand.
 *
 * That is not hypothetical: `weighing.observation.verified` and `weighing.shed.closed` were emitted
 * for months while only `weighing.shed.reopened` was in the enum. Reopen notifications worked;
 * verified and closed died silently, so a verifier's approval told nobody. Two independent E2E
 * lanes found it from opposite directions and the outbox confirmed it:
 *
 *   weighing.shed.reopened        | published
 *   weighing.observation.verified | failed | invalid_event_envelope
 *   weighing.shed.closed          | failed | invalid_event_envelope
 *
 * WHAT THIS CHECKS: Go constants of the shape `eventType<Name> = "<a.b.c>"` (and the
 * `Event<Name> = "..."` exported form) inside backend/internal/**, compared against the enum in
 * contracts/jsonschema/domain-event-envelope.schema.json.
 *
 * BLIND SPOTS, stated plainly: an event type built by string concatenation, held in a struct field,
 * or passed as a literal straight into a publish call is invisible here. This anchors on the
 * declaration convention the codebase already follows; it is not a call-graph analysis. It also
 * cannot tell whether a type in the enum is still emitted -- a stale enum entry is harmless, a
 * missing one is not, so the check is deliberately one-directional.
 *
 * Usage:
 *   node tools/agent-hooks/check-domain-event-envelope-enum.mjs
 *   node tools/agent-hooks/check-domain-event-envelope-enum.mjs --self-test
 */

import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { join, resolve, relative } from 'node:path'

const repo = resolve(import.meta.dirname, '../..')
const SCHEMA = 'contracts/jsonschema/domain-event-envelope.schema.json'
const SCAN_ROOT = 'backend/internal'

/** `eventTypeShedClosed = "weighing.shed.closed"` / `EventWeighingShedClosed = "..."`. */
const DECL = /(?:^|\s)(?:eventType|Event)[A-Za-z0-9_]*\s*=\s*"([a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+)"/g

function enumFromSchema(text) {
  const schema = JSON.parse(text)
  const values = schema?.properties?.event_type?.enum
  if (!Array.isArray(values)) throw new Error(`${SCHEMA}: properties.event_type.enum is missing`)
  return new Set(values)
}

export function declaredEventTypes(source) {
  const out = new Set()
  for (const m of source.matchAll(DECL)) out.add(m[1])
  return out
}

function goFiles(dir, acc = []) {
  if (!existsSync(dir)) return acc
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) goFiles(path, acc)
    else if (entry.endsWith('.go') && !entry.endsWith('_test.go')) acc.push(path)
  }
  return acc
}

export function findings(enumValues, sources) {
  const problems = []
  for (const [file, source] of sources) {
    for (const eventType of declaredEventTypes(source)) {
      if (!enumValues.has(eventType)) {
        problems.push(
          `${file}: emits "${eventType}" but it is absent from ${SCHEMA}. ` +
            `The write will succeed and the relay will drop it as invalid_event_envelope, so every ` +
            `consumer and notification for it silently never runs.`,
        )
      }
    }
  }
  return problems
}

function selfTest() {
  const enumValues = new Set(['weighing.shed.reopened'])

  const missing = findings(enumValues, [
    ['x.go', 'const (\n\teventTypeShedClosed = "weighing.shed.closed"\n)'],
  ])
  if (missing.length !== 1) throw new Error('self-test: an unlisted event type was not flagged')

  const present = findings(enumValues, [
    ['x.go', 'const eventTypeReopened = "weighing.shed.reopened"'],
  ])
  if (present.length !== 0) throw new Error('self-test: a listed event type was wrongly flagged')

  // Exported spelling is the same defect.
  const exported = findings(enumValues, [
    ['x.go', 'const EventWeighingObservationVerified = "weighing.observation.verified"'],
  ])
  if (exported.length !== 1) throw new Error('self-test: the exported Event* spelling was not checked')

  // Must NOT mistake a permission/route string for an event: those are not dotted event families
  // declared through this convention, and flagging them would make the guard unusable.
  const notAnEvent = findings(enumValues, [
    ['x.go', 'const CountsRead = "counts.read"\nconst routeWeighing = "/weighing/campaigns"'],
  ])
  if (notAnEvent.length !== 0) {
    throw new Error(`self-test: flagged a non-event constant: ${notAnEvent[0]}`)
  }

  console.log('domain-event-envelope-enum self-test: ok')
}

function main() {
  if (process.argv.includes('--self-test')) return selfTest()

  const enumValues = enumFromSchema(readFileSync(join(repo, SCHEMA), 'utf8'))
  const sources = goFiles(join(repo, SCAN_ROOT)).map((path) => [
    relative(repo, path),
    readFileSync(path, 'utf8'),
  ])

  const problems = findings(enumValues, sources)
  if (problems.length === 0) {
    console.log(`domain-event-envelope-enum: ok (${enumValues.size} enum entries, ${sources.length} files)`)
    return
  }
  console.error('\ndomain-event-envelope-enum FAILED — events emitted but not in the envelope enum:\n')
  for (const p of problems) console.error(`  - ${p}`)
  console.error(`\nAdd each to properties.event_type.enum in ${SCHEMA}.\n`)
  process.exit(1)
}

main()
