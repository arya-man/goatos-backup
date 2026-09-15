import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const repoRoot = fileURLToPath(new URL('../../..', import.meta.url));
const head = () => execFileSync('git', ['-C', repoRoot, 'rev-parse', 'HEAD'], {encoding: 'utf8'}).trim();
// Cloud build source archives may omit .git. Local certification requires a git SHA;
// ordinary deployment builds still use Next's build ID without a local receipt.
let sha;
try { sha = head(); } catch { sha = null; }
execFileSync('next', ['build', '--webpack', ...process.argv.slice(2)], {stdio: 'inherit'});
execFileSync(process.execPath, ['scripts/check-token-leak.mjs'], {stdio: 'inherit'});
if (sha) {
  if (sha !== head()) throw new Error('HEAD changed during admin-web build');
  writeFileSync('.next/local-build-provenance.json', JSON.stringify({
    git_sha: sha, build_id: readFileSync('.next/BUILD_ID', 'utf8').trim(), created_at: new Date().toISOString(),
  }, null, 2));
}
