#!/usr/bin/env node
/**
 * Design-system guard: no screen invents its own colours or type.
 *
 * The maintainer's rule is absolute — the app has ONE visual identity (greenish), and every colour,
 * text style and size in a Compose screen comes from the central design system (MeshaColors,
 * MeshaType, the design-system components). A literal `Color(0xFF...)` or a bare `fontSize = 13.sp`
 * in a feature screen is how an app drifts into looking like five different apps: one screen's grey
 * is not another's, a stray blue appears in a green product, and a translated label breaks a
 * hand-tuned size that no token governs.
 *
 * DIFF-SCOPED against origin/main, so a commit touching no Compose code passes instantly and this
 * never becomes a rewrite-the-world gate. It blocks NEW drift; existing debt is inventoried by the
 * audit, not by this guard.
 *
 * ALLOWED, deliberately:
 *   - core-designsystem itself: that is where the tokens are DEFINED.
 *   - Preview/screenshot fixtures and tests: not shipped UI.
 *   - `Color.Transparent` / `Color.Unspecified`: absence of colour, not a palette choice.
 *   - A line carrying `design-system:ignore: <reason>`.
 *
 * Usage:
 *   node tools/agent-hooks/check-design-system-tokens.mjs
 *   node tools/agent-hooks/check-design-system-tokens.mjs --self-test
 */

import { execSync } from 'node:child_process'
import { existsSync, readFileSync, mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

/**
 * Recorded debt. The rule arrived after the screens did, so the existing literals are inventoried
 * here rather than blocking every commit: this guard's job is to stop NEW drift while the backlog is
 * retired deliberately. Entries are `file:rule` — a line move does not resurrect a false failure,
 * but a NEW rule violation in the same file does fail.
 *
 * Shrink this file; never grow it. Regenerate with --write-baseline only when retiring debt.
 */
const BASELINE_PATH = join(dirname(fileURLToPath(import.meta.url)), 'design-system-baseline.txt')

function loadBaseline() {
  if (!existsSync(BASELINE_PATH)) return new Set()
  return new Set(
    readFileSync(BASELINE_PATH, 'utf8')
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith('#')),
  )
}

const baselineKey = (v) => `${v.file}:${v.rule}`

const IGNORE = 'design-system:ignore:'

/** Where tokens are allowed to be literal, because this is the source of the tokens. */
const EXEMPT_PATH_PARTS = [
  '/core/core-designsystem/',
  '/build/',
  '/src/test/',
  '/src/androidTest/',
  // Debug-only source set: compiled out of every release build, so nothing in it
  // is shipped UI. It holds the edge-case galleries whose whole job is to
  // REPRODUCE a production screen, including camera-viewfinder chrome that has no
  // token by design (translucent scrims over live video, a translucent REC pill).
  // Forcing tokens here would change what the gallery is meant to mirror while
  // protecting nothing a user can see. Same category as the Preview /
  // ScreenSamples.kt fixtures already listed below.
  '/src/debug/',
  '/screenshot/',
  'Preview',
  'ScreenSamples.kt',
]

const RULES = [
  {
    id: 'literal-color',
    // Color(0xFF1B5E20) or Color(red = ...) — a palette entry invented in a screen.
    re: /\bColor\s*\(\s*0x[0-9a-fA-F]{6,8}\b/,
    message: 'literal Color(0x…) in a screen — use a MeshaColors token',
  },
  {
    id: 'named-compose-color',
    // Color.Red / Color.White etc. Transparent and Unspecified are absence of colour, not palette.
    re: /\bColor\.(?!Transparent\b|Unspecified\b)[A-Z][A-Za-z]+\b/,
    message: 'Compose named colour in a screen — use a MeshaColors token',
  },
  {
    id: 'literal-font-size',
    re: /\bfontSize\s*=\s*\d+(\.\d+)?\.sp\b/,
    message: 'hardcoded fontSize — use a MeshaType style',
  },
  {
    id: 'literal-font-weight',
    re: /\bfontWeight\s*=\s*FontWeight\.(W\d{3}|Bold|SemiBold|Medium|Light|Thin|Black|ExtraBold)\b/,
    message: 'hardcoded fontWeight — use a MeshaType style',
  },
]

function changedKotlinFiles() {
  let base = 'origin/main'
  try {
    execSync(`git rev-parse --verify --quiet ${base}`, { stdio: 'ignore' })
  } catch {
    base = 'HEAD~1'
  }
  let out = ''
  try {
    out = execSync(`git diff --name-only --diff-filter=ACMR ${base}...HEAD`, { encoding: 'utf8' })
    out += execSync('git diff --name-only --diff-filter=ACMR HEAD', { encoding: 'utf8' })
    out += execSync('git ls-files --others --exclude-standard', { encoding: 'utf8' })
  } catch {
    return []
  }
  return [...new Set(out.split('\n').map((l) => l.trim()).filter(Boolean))]
    .filter((f) => f.endsWith('.kt'))
    .filter((f) => existsSync(f))
}

function isExempt(file) {
  const normalized = '/' + file.replace(/\\/g, '/')
  return EXEMPT_PATH_PARTS.some((part) => normalized.includes(part))
}

export function scanSource(file, source) {
  const violations = []
  source.split('\n').forEach((line, idx) => {
    if (line.includes(IGNORE)) return
    const code = line.split('//')[0]
    if (!code.trim()) return
    for (const rule of RULES) {
      if (rule.re.test(code)) {
        violations.push({ file, line: idx + 1, rule: rule.id, message: rule.message, text: line.trim().slice(0, 120) })
      }
    }
  })
  return violations
}

function selfTest() {
  const dir = mkdtempSync(join(tmpdir(), 'ds-guard-'))
  let failures = 0
  const expectHit = [
    ['Bad.kt', 'Text(color = Color(0xFF00FF00))'],
    ['Bad2.kt', 'Text(color = Color.Red)'],
    ['Bad3.kt', 'Text(fontSize = 13.sp)'],
    ['Bad4.kt', 'Text(fontWeight = FontWeight.W800)'],
  ]
  const expectClean = [
    ['Ok.kt', 'Text(color = MeshaColors.Ink, style = MeshaType.cardTitle)'],
    ['Ok2.kt', 'Box(Modifier.background(Color.Transparent))'],
    ['Ok3.kt', 'Text(color = Color.Red) // design-system:ignore: legacy splash, retired separately'],
  ]
  for (const [name, src] of expectHit) {
    const p = join(dir, name)
    writeFileSync(p, src)
    if (scanSource(name, readFileSync(p, 'utf8')).length === 0) {
      console.error(`self-test FAILED: expected a violation in ${name}: ${src}`)
      failures++
    }
  }
  for (const [name, src] of expectClean) {
    const p = join(dir, name)
    writeFileSync(p, src)
    const hits = scanSource(name, readFileSync(p, 'utf8'))
    if (hits.length > 0) {
      console.error(`self-test FAILED: expected NO violation in ${name}: ${src} -> ${hits[0].rule}`)
      failures++
    }
  }
  // Path exemptions: debug-only UI is out of scope, but a real feature screen
  // must never be able to borrow that exemption.
  const expectExempt = [
    'apps/goatos-android/app/src/debug/kotlin/sg/mesha/goatos/EdgeCaseGallerySamples.kt',
    'apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/Foo.kt',
  ]
  for (const file of expectExempt) {
    if (!isExempt(file)) {
      console.error(`self-test FAILED: expected ${file} to be exempt`)
      failures++
    }
  }
  const expectScanned = [
    'apps/goatos-android/feature/feature-counts/src/main/kotlin/sg/mesha/goatos/feature/counts/BirthDeathScreen.kt',
    'apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt',
  ]
  for (const file of expectScanned) {
    if (isExempt(file)) {
      console.error(`self-test FAILED: expected ${file} to be scanned, not exempt`)
      failures++
    }
  }

  rmSync(dir, { recursive: true, force: true })
  if (failures > 0) {
    console.error(`design-system guard self-test: ${failures} case(s) failed`)
    process.exit(1)
  }
  console.log('design-system guard self-test: all cases passed')
}

function main() {
  if (process.argv.includes('--self-test')) return selfTest()
  const files = changedKotlinFiles().filter((f) => !isExempt(f))
  const found = files.flatMap((f) => scanSource(f, readFileSync(f, 'utf8')))

  if (process.argv.includes('--write-baseline')) {
    const keys = [...new Set(found.map(baselineKey))].sort()
    writeFileSync(
      BASELINE_PATH,
      '# Design-system debt: existing literal colours/type predating the central-token rule.\n' +
        '# Shrink this list; never grow it. One entry per file:rule.\n' +
        keys.join('\n') + '\n',
    )
    console.log(`design-system baseline written: ${keys.length} entr(ies)`)
    return
  }

  const baseline = loadBaseline()
  const violations = found.filter((v) => !baseline.has(baselineKey(v)))
  if (violations.length === 0) {
    if (found.length > 0) {
      console.log(`design-system guard: ${found.length} known violation(s) in baseline, no new drift`)
      return
    }
    console.log(`design-system guard: ${files.length} changed Kotlin file(s) clean`)
    return
  }
  console.error('\nDESIGN SYSTEM VIOLATIONS — every colour and text style comes from the central design system.\n')
  for (const v of violations) {
    console.error(`  ${v.file}:${v.line}  [${v.rule}] ${v.message}`)
    console.error(`      ${v.text}`)
  }
  console.error(`\n${violations.length} violation(s). Use MeshaColors / MeshaType, or annotate the line`)
  console.error(`with "${IGNORE} <reason>" when a literal is genuinely correct.\n`)
  process.exit(1)
}

main()
