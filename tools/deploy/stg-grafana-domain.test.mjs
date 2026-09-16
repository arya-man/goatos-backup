import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";
const script = fileURLToPath(new URL("./stg-clouddeploy-task.sh", import.meta.url));
function fixture(command, extra = {}) {
  const dir = mkdtempSync(path.join(tmpdir(), "grafana-domain-"));
  const mock = `#!/usr/bin/env python3
import os,sys,json,pathlib
args=sys.argv[1:];cmd=' '.join(args);root=pathlib.Path(os.environ['FIXTURE'])
with open(root/'calls','a') as f:f.write(pathlib.Path(sys.argv[0]).name+' '+cmd+'\\n')
changed=(root/'updated').exists()
if pathlib.Path(sys.argv[0]).name=='curl':
 print('401' if '/api/search' in cmd else ('404' if 'raw.run.app' in cmd else '200'),end='');sys.exit()
if 'ssl-certificates describe' in cmd:print(os.environ.get('CERT_STATUS','ACTIVE'))
elif 'run services update ' in cmd:(root/'updated').touch()
elif 'run revisions describe' in cmd:print(json.dumps({'status':{'conditions':[{'type':'Ready','status':'True'}]}}))
elif 'run services describe' in cmd:
 if 'value(status.url)' in cmd:print('https://raw.run.app')
 elif 'value(status.latestCreatedRevisionName)' in cmd:print('grafana-new')
 else:
  env=[{'name':'GF_AUTH_ANONYMOUS_ENABLED','value':os.environ.get('ANONYMOUS','false')},{'name':'GF_USERS_ALLOW_SIGN_UP','value':'false'},{'name':'GF_SECURITY_ADMIN_PASSWORD','valueFrom':{'secretKeyRef':{'name':'password'}}},{'name':'GF_SERVER_ROOT_URL','value':'https://grafana.mesha.sg/' if changed else 'https://raw.run.app'}]
  print(json.dumps({'metadata':{'annotations':{'run.googleapis.com/ingress':'internal-and-cloud-load-balancing' if changed else 'all'}},'spec':{'template':{'spec':{'containers':[{**({'name':'grafana'} if os.environ.get('UNNAMED') != 'true' else {}),'image':'grafana@sha256:same','env':env}]}}}}))
elif 'storage cp' in cmd or 'run services update-traffic' in cmd:pass
else:sys.exit('unexpected gcloud '+cmd)
`;
  for (const name of ["gcloud", "curl"]) writeFileSync(path.join(dir, name), mock, { mode: 0o755 });
  try {
    const result = spawnSync("bash", [script, command], { cwd: dir, encoding: "utf8", env: { ...process.env, PATH: `${dir}:${process.env.PATH}`, FIXTURE: dir, CLOUD_DEPLOY_customTarget_commitSha: "abcdef123456", CLOUD_DEPLOY_customTarget_grafanaDomainOnly: "true", CLOUD_DEPLOY_OUTPUT_GCS_PATH: "gs://fixture", ...extra } });
    return { ...result, calls: readFileSync(path.join(dir, "calls"), "utf8") };
  } finally { rmSync(dir, { recursive: true, force: true }); }
}
test("domain-only render requires no application images", () => {
  const r = fixture("render"); assert.equal(r.status, 0, r.stderr); assert.match(r.calls, /goatos-stg-grafana-domain.txt/); assert.doesNotMatch(r.calls, /artifacts docker|run services update/);
});
test("domain cutover changes only Grafana and waits before traffic", () => {
  const r = fixture("deploy"); assert.equal(r.status, 0, r.stderr); assert.match(r.stdout, /cloud-deploy-grafana-domain-ok/);
  assert.doesNotMatch(r.calls, /run jobs|goatos-api-stg|goatos-admin-web-stg|--image/);
  assert.ok(r.calls.indexOf("run revisions describe") < r.calls.indexOf("run services update-traffic"));
  assert.match(r.calls, /--ingress=internal-and-cloud-load-balancing/);
  assert.match(r.calls, /--update-env-vars=GF_SERVER_ROOT_URL=https:\/\/grafana.mesha.sg\//);
});
for (const [name, env] of [["pending certificate", { CERT_STATUS: "PROVISIONING" }], ["anonymous Grafana", { ANONYMOUS: "true" }]]) {
  test(`${name} fails before mutation`, () => { const r = fixture("deploy", env); assert.notEqual(r.status, 0); assert.doesNotMatch(r.calls, /run services update/); });
}

test("routine deploy smoke follows the protected custom domain", () => {
  const caller = readFileSync(new URL("./stg-cloudbuild-release.sh", import.meta.url), "utf8");
  const section = caller.slice(caller.indexOf("smoke_grafana_dashboards()"), caller.indexOf("on_exit()"));
  assert.match(section, /--url https:\/\/grafana.mesha.sg/);
  assert.match(section, /--no-proxy/);
  assert.doesNotMatch(section, /--direct-iam/);
});

test("domain cutover accepts the actual singleton unnamed Cloud Run container", () => {
  const r = fixture("deploy", { UNNAMED: "true" });
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /cloud-deploy-grafana-domain-ok/);
});

test("runner Dockerfile restores executable mode for archived source files", () => {
  const dockerfile = readFileSync(new URL("../../deploy/clouddeploy/stg/runner.Dockerfile", import.meta.url), "utf8");
  const copy = dockerfile.lastIndexOf("COPY tools/deploy/");
  const chmod = dockerfile.indexOf("RUN chmod 0755 /usr/local/bin/goatos-stg-clouddeploy-task");
  assert.ok(chmod > copy);
  assert.match(dockerfile.slice(chmod), /\/usr\/local\/bin\/goatos-stg-analytics-events-routing/);
  assert.ok(chmod < dockerfile.indexOf("ENTRYPOINT"));
});

for (const fail of [false, true]) {
  test(`runner image smoke ${fail ? "failure blocks publication" : "runs before publication"}`, () => {
    const dir = mkdtempSync(path.join(tmpdir(), "grafana-runner-"));
    const bin = `#!/usr/bin/env python3
import os,sys,pathlib
name=pathlib.Path(sys.argv[0]).name;args=sys.argv[1:];root=pathlib.Path(os.environ['FIXTURE'])
with open(root/'calls','a') as f:f.write(name+' '+' '.join(args)+'\\n')
if name=='git':print(str(root) if '--show-toplevel' in args else 'abcdef123456')
if name=='docker' and args[0]=='run' and os.environ.get('FAIL_SMOKE')=='true':sys.exit(1)
if name=='gcloud' and args[:4]==['artifacts','docker','images','describe']:print('sha256:verified')
`;
    for (const name of ["git", "docker", "gcloud"]) writeFileSync(path.join(dir, name), bin, { mode: 0o755 });
    try {
      const result = spawnSync("bash", [fileURLToPath(new URL("./stg-clouddeploy-runner-build.sh", import.meta.url))], { cwd: dir, encoding: "utf8", env: { ...process.env, PATH: `${dir}:${process.env.PATH}`, FIXTURE: dir, FAIL_SMOKE: String(fail) } });
      const calls = readFileSync(path.join(dir, "calls"), "utf8");
      assert.match(calls, /docker run --rm --platform linux\/amd64/);
      assert.match(calls, /test -x \/usr\/local\/bin\/goatos-stg-clouddeploy-task/);
      assert.match(calls, /goatos-stg-clouddeploy-task executable-smoke/);
      if (fail) { assert.notEqual(result.status, 0); assert.doesNotMatch(calls, /docker push|artifacts docker images describe/); }
      else { assert.equal(result.status, 0, result.stderr); assert.ok(calls.indexOf("docker run") < calls.indexOf("docker push")); assert.match(result.stdout, /sha256:verified/); }
    } finally { rmSync(dir, { recursive: true, force: true }); }
  });
}

test("Cloud Deploy actAs is restricted to the actual Grafana runtime identity", () => {
  const terraform = readFileSync(new URL("../../infra/envs/stg/observability.tf", import.meta.url), "utf8");
  const runtime = terraform.match(/resource "google_service_account" "grafana" \{([\s\S]*?)\n\}/)?.[1];
  assert.match(runtime ?? "", /account_id\s*=\s*"goatos-grafana-stg"/);
  const binding = terraform.match(/resource "google_service_account_iam_member" "grafana_clouddeploy_act_as" \{([\s\S]*?)\n\}/)?.[1];
  assert.match(binding ?? "", /service_account_id\s*=\s*google_service_account.grafana.name/);
  assert.match(binding ?? "", /role\s*=\s*"roles\/iam.serviceAccountUser"/);
  assert.match(binding ?? "", /member\s*=\s*"serviceAccount:\$\{google_service_account.github_deployer.email\}"/);
});
