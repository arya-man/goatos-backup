import {readFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import test from 'node:test';
import assert from 'node:assert/strict';
const source=readFileSync(new URL('./stg-clouddeploy-task.sh',import.meta.url),'utf8');
const fn=source.match(/normal_observability_deploy\(\) \{[\s\S]*?\n\}/)[0];
function execute(image='backend',fail=false){return spawnSync('bash',['-c',`set -euo pipefail
ALLOY_IMAGE=immutable
BACKEND_IMAGE=backend
PROJECT_ID=goatos-stg
REGION=asia-south1
job_image(){ echo '${image}'; }
die(){ exit 9; }
run(){ echo "$*"; ${fail?'return 8':'return 0'}; }
observability_apply_and_smoke(){ echo 'apply-and-strict-smoke'; }
${fn}
normal_observability_deploy
echo success`],{encoding:'utf8'});}
test('normal deployment waits for seven-day rollup before provisioning and query validation',()=>{const r=execute();assert.equal(r.status,0,r.stderr);assert.match(r.stdout,/jobs execute.*lookback-days=7.*--wait/);assert.ok(r.stdout.indexOf('jobs execute')<r.stdout.indexOf('apply-and-strict-smoke'));});
test('failed rollup prevents assets and success',()=>{const r=execute('backend',true);assert.equal(r.status,8);assert.doesNotMatch(r.stdout,/apply-and-strict-smoke|success/);});
test('wrong job image prevents execution',()=>{const r=execute('old');assert.equal(r.status,9);assert.equal(r.stdout,'');});
test('normal deploy hook follows settled images and precedes success receipt',()=>{const start=source.indexOf('  normal_observability_deploy\n');assert.ok(start>source.indexOf('image did not settle'));assert.ok(start<source.indexOf('  write_results "SUCCEEDED"',start));const body=source.match(/observability_apply_and_smoke\(\) \{[\s\S]*?\n\}/)[0];assert.match(body,/--query-validity-only/);assert.match(body,/full-data certification remains pending/);assert.doesNotMatch(body,/write_results/);});

test('worker dispatch env and scoped IAM preflight precede mutations',()=>{
 assert.match(source,/GOATOS_WORKER_STAGES_ENABLED=true,GOATOS_ANALYTICS_ROLLUP_JOB=projects\/\$\{PROJECT_ID\}\/locations\/\$\{REGION\}\/jobs\/goatos-stg-analytics-rollup/);
 const body=source.match(/assert_analytics_worker_iam\(\) \{[\s\S]*?\n\}/)[0];
 assert.match(body,/jobs get-iam-policy goatos-stg-analytics-rollup/);
 assert.match(body,/roles\/run.jobsExecutorWithOverrides/);
 assert.match(body,/roles\/run.viewer/);
 const deploy=source.slice(source.indexOf('\ndeploy() {'));
 assert.ok(deploy.indexOf('  assert_analytics_worker_iam')<deploy.indexOf('  local backend_prefix'));
});
test('worker IAM rejects missing or conditional grant and accepts exact scoped pair',()=>{
 const fn=source.match(/assert_analytics_worker_iam\(\) \{[\s\S]*?\n\}/)[0];
 for(const mode of ['missing','conditional','valid']){
  const bindings=['roles/run.jobsExecutorWithOverrides','roles/run.viewer'].map(role=>({role,members:['serviceAccount:worker@goatos-stg.iam.gserviceaccount.com'],...(mode==='conditional'?{condition:{expression:'true'}}:{})}));
  const policy=JSON.stringify({bindings:mode==='missing'?[]:bindings});
  const r=spawnSync('bash',['-c',`set -euo pipefail
PROJECT_ID=goatos-stg
REGION=asia-south1
KERNEL_WORKER_SERVICE=worker
gcloud(){ if [[ "$2" == services ]]; then echo worker@goatos-stg.iam.gserviceaccount.com; else printf '%s' '${policy}'; fi; }
die(){ exit 9; }
${fn}
assert_analytics_worker_iam`],{encoding:'utf8'});
  assert.equal(r.status===0,mode==='valid',r.stderr);
 }
});

test('packaged receipt and explicit deployment versus data certification modes',()=>{
 const docker=readFileSync(new URL('../../deploy/clouddeploy/stg/runner.Dockerfile',import.meta.url),'utf8');
 assert.match(docker,/COPY infra\/observability\/firebase-initial-export.json \/opt\/goatos\/infra\/observability\/firebase-initial-export.json/);
 assert.match(source,/--firebase-initial-export-receipt "\$receipt"/);
 const wrapper=readFileSync(new URL('./stg-cloudbuild-release.sh',import.meta.url),'utf8');
 assert.match(wrapper,/--query-validity-only/);
 assert.match(wrapper,/full-data readiness is pending separate certification/);
});
