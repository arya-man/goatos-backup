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
  if (receipt.mode === 'start') validateBuildProvenance(receipt.build_provenance, receipt.build_provenance?.build_id, expected.git_sha);
  if (!receipt.created_at || !Number.isFinite(Date.parse(receipt.created_at))) throw new Error('Missing launch timestamp');
  if (!receipt.process_identity || identity(receipt.pid) !== receipt.process_identity) throw new Error('Local stack process stopped or PID was reused');
  return receipt;
}

export function validateBuildProvenance(provenance, buildId, sha) {
  if (!provenance || !buildId || provenance.build_id !== buildId || provenance.git_sha !== sha) {
    throw new Error('Admin-web .next build does not match current HEAD; run npm run build before certification');
  }
  if (provenance.clean_source !== true || !cleanBuildSource(provenance.source_status_start, provenance.source_status_end)) throw new Error('Admin-web build used dirty or unverified source; build from clean HEAD before certification');
  return provenance;
}

export function sourceStatus(repoRoot) {
  // Git excludes ignored .next and local evidence artifacts by default.
  return execFileSync('git', ['-C', repoRoot, 'status', '--porcelain=v1', '--untracked-files=all'], {encoding: 'utf8'}).split('\n').filter(Boolean);
}
export function cleanBuildSource(before, after) {
  return Array.isArray(before) && Array.isArray(after) && before.length === 0 && after.length === 0;
}

export function localRuntimeActor(env) {
  const tenant_id = (env.GOATOS_TENANT_ID ?? '').trim();
  const local_user_id = (env.GOATOS_LOCAL_USER_ID ?? '90000000-0000-4000-8000-000000000101').trim();
  const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  if (!uuid.test(tenant_id) || !uuid.test(local_user_id)) throw new Error('Local runtime actor must use tenant and user UUIDs');
  return {tenant_id, local_user_id};
}
export function validateSmokeActor(receipt, token, tenant) {
  let claims;
  try {claims = JSON.parse(Buffer.from(token.split('.')[1], 'base64url').toString());}
  catch {throw new Error('Browser proof requires a readable JWT identity');}
  const actor = localRuntimeActor({GOATOS_TENANT_ID: receipt?.tenant_id, GOATOS_LOCAL_USER_ID: receipt?.local_user_id ?? ''});
  if (claims.sub !== actor.local_user_id || claims.tenant_id !== actor.tenant_id || tenant !== actor.tenant_id) {
    throw new Error('Browser bearer actor/tenant differs from frontend runtime actor');
  }
  return {user_id: actor.local_user_id, tenant_id: actor.tenant_id};
}
