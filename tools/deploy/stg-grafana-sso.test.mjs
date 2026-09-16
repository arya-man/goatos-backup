import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,readFileSync,existsSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import test from 'node:test';
const here=path.dirname(fileURLToPath(import.meta.url));
const script=path.join(here,'stg-clouddeploy-task.sh');
function fixture(command,extra={}) {
 const dir=mkdtempSync(path.join(tmpdir(),'grafana-sso-'));
 const mock=`#!/usr/bin/env python3
import os,sys,json,pathlib,runpy
args=sys.argv[1:];cmd=' '.join(args);root=pathlib.Path(os.environ['FIXTURE']);helper=runpy.run_path(os.environ['HELPER'])
with open(root/'calls','a') as f:f.write(pathlib.Path(sys.argv[0]).name+' '+cmd+'\\n')
if pathlib.Path(sys.argv[0]).name=='curl':
 if '-D' in args:
  (root/args[args.index('-D')+1]).write_text('Location: https://accounts.google.com/o/oauth2/v2/auth?redirect_uri=https%3A%2F%2Fgrafana.mesha.sg%2Flogin%2Fgeneric_oauth&state=s&client_id=c&code_challenge=c&code_challenge_method=S256\\r\\n')
  print('302',end='')
 else: print('401' if '/api/search' in cmd else ('404' if 'raw.run.app' in cmd else '200'),end='')
 sys.exit()
if 'run services update ' in cmd:
 envarg=next(a for a in args if a.startswith('--update-env-vars=')); assert envarg.startswith('--update-env-vars=^~^')
 values=dict(item.split('=',1) for item in envarg.split('^~^',1)[1].split('~'));assert values==helper['ENV']
 (root/'updated').touch()
elif 'run revisions describe' in cmd: print(json.dumps({'status':{'conditions':[{'type':'Ready','status':'True'}]}}))
elif 'run services describe' in cmd:
 if 'value(status.url)' in cmd:print('https://raw.run.app')
 elif 'value(status.latestCreatedRevisionName)' in cmd:print('grafana-sso-new')
 else:
  e=[{'name':'GF_AUTH_ANONYMOUS_ENABLED','value':os.environ.get('ANONYMOUS','false')},{'name':'GF_USERS_ALLOW_SIGN_UP','value':'false'},{'name':'GF_SERVER_ROOT_URL','value':'https://grafana.mesha.sg/'},{'name':'GF_SECURITY_ADMIN_PASSWORD','valueFrom':{'secretKeyRef':{'name':'password','key':'latest'}}}]
  changed=(root/'updated').exists()
  if changed:
   e += [{'name':k,'value':v} for k,v in helper['ENV'].items()]
   e += [{'name':k,'valueFrom':{'secretKeyRef':{'name':v,'key':'latest'}}} for k,v in helper['SECRETS'].items()]
  c={'image':'grafana/grafana:11.3.0','env':e}
  if changed and os.environ.get('DRIFT')=='true':c['image']='wrong'
  print(json.dumps({'metadata':{'annotations':{'run.googleapis.com/ingress':'internal-and-cloud-load-balancing'}},'spec':{'template':{'spec':{'containers':[c]}}}}))
elif 'storage cp' in cmd or 'run services update-traffic' in cmd:pass
else:sys.exit('unexpected command '+cmd)
`;
 for(const bin of ['gcloud','curl'])writeFileSync(path.join(dir,bin),mock,{mode:0o755});
 try {
  const r=spawnSync('bash',[script,command],{cwd:dir,encoding:'utf8',env:{...process.env,PATH:dir+':'+process.env.PATH,FIXTURE:dir,HELPER:path.join(here,'stg-grafana-sso.py'),CLOUD_DEPLOY_customTarget_commitSha:'abcdef123456',CLOUD_DEPLOY_customTarget_grafanaSsoOnly:'true',CLOUD_DEPLOY_OUTPUT_GCS_PATH:'gs://fixture',...extra}});
  return {...r,calls:existsSync(path.join(dir,'calls'))?readFileSync(path.join(dir,'calls'),'utf8'):''};
 } finally {rmSync(dir,{recursive:true,force:true});}
}
test('SSO render requires no app images and mutates no service',()=>{const r=fixture('render');assert.equal(r.status,0,r.stderr);assert.match(r.calls,/goatos-stg-grafana-sso.txt/);assert.doesNotMatch(r.calls,/run services update|artifacts docker/);});
test('SSO deployment is Grafana-only, secret-backed, and verifies before pinned traffic',()=>{const r=fixture('deploy');assert.equal(r.status,0,r.stderr);assert.match(r.stdout,/cloud-deploy-grafana-sso-ok/);assert.doesNotMatch(r.calls,/run jobs|goatos-api-stg|goatos-admin-web-stg|--image/);assert.match(r.calls,/--to-revisions=grafana-sso-new=100/);assert.match(r.calls,/--update-secrets=GF_AUTH_GENERIC_OAUTH_CLIENT_ID=goatos-stg-grafana-oauth-client-id:latest/);assert.ok(r.calls.lastIndexOf('run services describe')<r.calls.indexOf('run services update-traffic'));});
test('runtime drift blocks traffic',()=>{const r=fixture('deploy',{DRIFT:'true'});assert.notEqual(r.status,0);assert.doesNotMatch(r.calls,/update-traffic/);});
for(const env of [{ANONYMOUS:'true'},{CLOUD_DEPLOY_customTarget_grafanaDomainOnly:'true'},{CLOUD_DEPLOY_customTarget_grafanaSsoOnly:'invalid'}])test('unsafe/ambiguous input rejects before mutation '+JSON.stringify(env),()=>{const r=fixture('deploy',env);assert.notEqual(r.status,0);assert.doesNotMatch(r.calls,/run services update/);});
test('helper preserves configuration and rejects unsafe readback/redirects',()=>{const r=spawnSync('python3',[path.join(here,'stg-grafana-sso_test.py')],{encoding:'utf8'});assert.equal(r.status,0,r.stderr);});
test('actual Go JMESPath evaluates four verified emails and rejects other claims',{timeout:120000},()=>{const r=spawnSync('go',['test','-count=1','./...'],{cwd:path.join(here,'grafana-sso-policy-test'),encoding:'utf8',timeout:110000});assert.equal(r.status,0,r.stderr);});
test('both runner paths smoke packaged SSO helper before publishing',()=>{
 const docker=readFileSync(path.join(here,'../../deploy/clouddeploy/stg/runner.Dockerfile'),'utf8');assert.match(docker,/COPY tools\/deploy\/stg-grafana-sso.py \/usr\/local\/bin\/goatos-stg-grafana-sso.py/);
 for(const file of ['stg-clouddeploy-runner-build.sh','../../cloudbuild.stg-runner.yaml']){const s=readFileSync(path.join(here,file),'utf8');assert.ok(s.indexOf('python3 -m py_compile /usr/local/bin/goatos-stg-grafana-sso.py')<s.indexOf('docker push'));assert.match(s,/goatos-stg-clouddeploy-task executable-smoke/);}
 assert.match(readFileSync(path.join(here,'../../cloudbuild.stg-runner.yaml'),'utf8'),/_RUNNER_TAG=\$\{_RUNNER_TAG\}/);
});
