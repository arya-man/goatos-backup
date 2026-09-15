import { execFileSync } from 'node:child_process';

export function processIdentity(pid) {
  if (!Number.isInteger(pid) || pid <= 0) throw new Error('Invalid local server PID');
  process.kill(pid, 0);
  return execFileSync('ps', ['-p', String(pid), '-o', 'lstart='], {encoding: 'utf8'}).trim();
}

export function validateLocalStackReceipt(receipt, expected, identity = processIdentity) {
  if (!receipt || receipt.schema_version !== 1) throw new Error('Missing local stack launch receipt');
  for (const key of ['git_sha', 'api_base_url', 'admin_web_base_url']) {
    if (!receipt[key] || receipt[key] !== expected[key]) throw new Error(`Local stack receipt ${key} mismatch`);
  }
  if (!receipt.created_at || !Number.isFinite(Date.parse(receipt.created_at))) throw new Error('Missing launch timestamp');
  if (!receipt.process_identity || identity(receipt.pid) !== receipt.process_identity) throw new Error('Local stack process stopped or PID was reused');
  return receipt;
}
