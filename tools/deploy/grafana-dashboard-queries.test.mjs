import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { panelQueries, assertQueryResults } from './smoke-stg-grafana-dashboards.mjs';
const dashboard = (name) => JSON.parse(readFileSync(new URL(`../../infra/grafana/dashboards/${name}.json`, import.meta.url)));

test('kernel retry and failure panels use the consolidated worker counters', async () => {
  const {conditionalDataReason} = await import('./smoke-stg-grafana-dashboards.mjs');
  const doc = dashboard('03-kernel-pipeline');
  for (const [id, names, source] of [
    [6, ['kernel_outbox_reclaimed_total', 'kernel_outbox_retry_scheduled_total', 'kernel_outbox_dead_letters_total'], 'backend/internal/platform/kmetrics/outbox.go'],
    [14, ['kernel_notify_failures_total', 'kernel_notify_exhausted_total'], 'backend/internal/platform/kmetrics/notify.go'],
  ]) {
    const targets = doc.panels.find(p => p.id === id).targets;
    assert.equal(targets.length, names.length);
    for (const [i, target] of targets.entries()) {
      assert.equal(target.queryType, 'promQL');
      assert.equal(target.promQLQuery.expr, `sum(rate(${names[i]}[5m]))`);
      assert.equal(target.goatosDataCondition.source, source);
      assert.match(conditionalDataReason(target), /emitter:/);
      assert.doesNotMatch(JSON.stringify(target), /cloud_run_job|logging.googleapis.com/);
      const wrongSource = structuredClone(target);
      wrongSource.goatosDataCondition.source = 'backend/cmd/retired-worker/main.go';
      assert.throws(() => conditionalDataReason(wrongSource), /Invalid conditional/);
    }
  }
});

test('mixed success cannot conceal a failed target or missing result', () => {
  const queries = [{refId:'A'}, {refId:'B'}];
  const good = {status:200, frames:[{data:{values:[[1],[42]]}}]};
  assert.throws(() => assertQueryResults({results:{A:good,B:{error:'invalid histogram rate'}}},queries,'latency'), /B: invalid histogram rate/);
  assert.throws(() => assertQueryResults({results:{A:good}},queries,'latency'), /B: missing query result/);
  assert.doesNotThrow(() => assertQueryResults({results:{A:good,B:{status:200,frames:[]}}},queries,'latency'));
});

test('actual SLO panel uses full selected range and pinned end, preserves Grafana11.3 migration sentinel', () => {
  const doc=dashboard('06-slo-burn');
  const variable=doc.templating.list.find(v=>v.name==='window_seconds');
  assert.equal(variable.refresh,2);
  assert.match(variable.query,/\$\{__to\}::bigint - \$\{__from\}::bigint/);
  for (const id of [1,2]) {
    const panel=doc.panels.find(p=>p.id===id);
    for (const seconds of [3600,86400,604800]) {
      const [q]=panelQueries(panel,'goatos-stg',{fromMs:1000000,now:1000000+seconds*1000});
      assert.deepEqual(q.timeSeriesList,{});
      assert.equal(q.queryType,'promQL');
      assert.ok(q.promQLQuery.expr.includes(`[${seconds}s] @ end()`));
      assert.ok(!q.promQLQuery.expr.includes('${'));
      assert.ok(!q.promQLQuery.expr.includes('rate(1h)'));
      assert.ok(!q.promQLQuery.expr.includes('or vector(0)'));
    }
  }
});

test('actual mobile cohort panels distinguish absent completions and use tenant-bound read queries', () => {
  const doc=dashboard('05-mobile');
  for (const id of [4,6]) {
    const [q]=panelQueries(doc.panels.find(p=>p.id===id),'goatos-stg',{variables:{'${tenant:sqlstring}':"'00000000-0000-4000-8000-000000000001'"}});
    assert.equal(q.datasource.uid,'postgres-analytics');
    assert.match(q.rawSql,/tenant_id = '00000000-0000-4000-8000-000000000001'::uuid/);
    assert.match(q.rawSql,/AT TIME ZONE 'Asia\/Kolkata'/);
    assert.ok(q.rawSql.includes("$__timeFilter(((event_date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata') - interval '1 second')"));
    if(id===4)assert.match(q.rawSql,/CASE WHEN completions > 0 THEN/);
  }
});

test('no histogram distribution is passed directly to rate and latency precedes traffic', () => {
  for (const name of ['03-kernel-pipeline','06-slo-burn']) {
    for(const panel of dashboard(name).panels)for(const t of panel.targets??[]) {
      assert.doesNotMatch(t.timeSeriesQuery?.query??'',/\/histogram'\s*\|\s*align rate/);
      if(t.timeSeriesList?.filters?.some(v=>v.endsWith?.('/histogram')))assert.notEqual(t.timeSeriesList.perSeriesAligner,'ALIGN_RATE');
    }
  }
  const panels=dashboard('00-feature-health').panels;
  for(const latency of panels.filter(p=>p.title.includes(': p95 latency by route'))) {
    const traffic=panels.find(p=>p.title===latency.title.replace('p95 latency','request rate'));
    assert.equal(latency.gridPos.x,0);assert.equal(traffic.gridPos.x,12);
  }
});

test('only source-proven event counters may treat no observation as conditional', async () => {
  const {conditionalDataReason}=await import('./smoke-stg-grafana-dashboards.mjs');
  const doc=dashboard('03-kernel-pipeline');
  const actual=doc.panels.flatMap(p=>p.targets??[]).find(t=>t.goatosDataCondition);
  assert.match(conditionalDataReason(actual),/emitter:/);
  const hiddenGap=structuredClone(actual);hiddenGap.promQLQuery.expr='sum(rate(kernel_consumer_lag_seconds[5m]))';
  assert.throws(()=>conditionalDataReason(hiddenGap),/Invalid conditional/);
  const mixed=structuredClone(actual);mixed.promQLQuery.expr+=' + kernel_consumer_lag_seconds';
  assert.throws(()=>conditionalDataReason(mixed),/Invalid conditional/);
  const inventedZero=structuredClone(actual);inventedZero.promQLQuery.expr+=' or vector(0)';
  assert.throws(()=>conditionalDataReason(inventedZero),/Invalid conditional/);
});

test('live readback cannot certify stale queries behind matching dashboard UIDs', async () => {
  const {assertDashboardQueryReadback}=await import('./smoke-stg-grafana-dashboards.mjs');
  const expected=dashboard('06-slo-burn');
  const actual=structuredClone(expected);
  assertDashboardQueryReadback(expected,actual);
  actual.panels.find(p=>p.id===1).targets[0].promQLQuery.expr='vector(100)';
  assert.throws(()=>assertDashboardQueryReadback(expected,actual),/do not match/);
});

test('browser panels use declared Faro metrics and actual bounded browser label', () => {
  const definitions=JSON.parse(readFileSync(new URL('../../infra/observability/faro-log-metrics.json',import.meta.url)));
  const byName=new Map(definitions.map(x=>[x.name,x]));
  const panels=dashboard('04-frontend-rum').panels;
  for(const panel of panels.filter(p=>p.id>=3&&p.id<=8))for(const target of panel.targets??[]) {
    if(target.timeSeriesList?.filters?.length) {
      const metric=target.timeSeriesList.filters[2].replace('logging.googleapis.com/user/','');
      assert.ok(byName.has(metric),`undeclared browser metric ${metric}`);
    } else {
      const names=target.promQLQuery.expr.match(/logging_googleapis_com:user_(\w+)/g)??[];
      assert.ok(names.length);
      for(const name of names)assert.ok(byName.has(name.replace('logging_googleapis_com:user_','')));
    }
  }
  const browser=panels.find(p=>p.id===8).targets[0];
  assert.match(browser.promQLQuery.expr,/sum by \(browser\)/);
  assert.ok(byName.get('goatos_rum_session_starts').metricDescriptor.labels.some(x=>x.key==='browser'));
});

test('Firebase exports are app-wide with observed denominators, not tenant proxies', () => {
  const panels=dashboard('05-mobile').panels;
  for(const [id,table,denominator] of [[1,'app_network_daily','total_requests'],[5,'app_crash_daily','total_sessions']]) {
    const panel=panels.find(p=>p.id===id);
    const sql=panel.targets[0].rawSql;
    assert.ok(sql.includes(`analytics.${table}`));
    assert.ok(sql.includes("source_app_id = 'sg.mesha.goatos'"));
    assert.ok(sql.includes(`${denominator} > 0`));
    assert.doesNotMatch(sql,/tenant_id|COALESCE/i);
    assert.equal(panel.goatosDataCondition,undefined);
  }
});

test('actual daily SQL places yesterday inside a midday last-24h view and switches at IST midnight', {skip:!process.env.GOATOS_DASHBOARD_TEST_DSN}, () => {
  const doc=dashboard('05-mobile');
  const expression=doc.panels.find(p=>p.id===1).targets[0].rawSql.match(/^SELECT (.*?) AS time,/)[1];
  for(const panel of doc.panels.filter(p=>[1,4,5,6,7,8,9].includes(p.id))) {
    assert.ok(panel.targets[0].rawSql.includes(`$__timeFilter(${expression})`),`panel${panel.id} does not filter its displayed bucket end`);
  }
  const rows=[['2026-09-16T14:00:00+05:30','2026-09-15'],['2026-09-16T23:59:58+05:30','2026-09-15'],['2026-09-17T00:00:00+05:30','2026-09-16']];
  for(const [to,expected] of rows) {
    const sql=`SELECT event_date FROM (VALUES (date '2026-09-14'),(date '2026-09-15'),(date '2026-09-16')) f(event_date) WHERE ${expression} BETWEEN timestamptz '${to}' - interval '24 hours' AND timestamptz '${to}' ORDER BY event_date`;
    const output=execFileSync('psql',[process.env.GOATOS_DASHBOARD_TEST_DSN,'-X','-A','-t','-v','ON_ERROR_STOP=1','-c',sql],{encoding:'utf8',env:{...process.env,PGOPTIONS:'-c statement_timeout=5000'}}).trim();
    assert.equal(output,expected);
  }
});

test('rollup health rejects never-completed, stale and failed jobs despite rows', async () => {
  const {assertQueryHealth}=await import('./smoke-stg-grafana-dashboards.mjs');
  const frame=(name,value)=>({frames:[{schema:{fields:[{name}]},data:{values:[[value]]}}]});
  const age=dashboard('05-mobile').panels.find(p=>p.id===10).targets[0];
  assert.doesNotThrow(()=>assertQueryHealth(frame('age_hours',2),age,'rollup'));
  for(const value of [null,31,-1])assert.throws(()=>assertQueryHealth(frame('age_hours',value),age,'rollup'),/never completed|stale/);
  const status=dashboard('05-mobile').panels.find(p=>p.id===11).targets[0];
  assert.doesNotThrow(()=>assertQueryHealth(frame('status','succeeded'),status,'rollup'));
  for(const value of ['failed','running','skipped','Never run'])assert.throws(()=>assertQueryHealth(frame('status',value),status,'rollup'),/latest rollup status/);
});

test('Firebase initial-export readiness is live-bound, expires, and never covers query failures', async () => {
 const {verifyFirebaseInitialExport,firebasePendingReason,assertQueryResults}=await import('./smoke-stg-grafana-dashboards.mjs');
 const now=Date.parse('2026-09-17T00:00:00Z'),created=now-3600000;
 const ids=['firebase_crashlytics','firebase_sessions','firebase_performance'];
 const sources=Object.fromEntries(JSON.parse(readFileSync(new URL('../../infra/observability/firebase-initial-export.json',import.meta.url))).datasets.map(d=>[d.id,d.dataSourceId]));
 const receipt={schemaVersion:1,project:'goatos-stg',appId:'sg.mesha.goatos',firebaseAppId:'1:514832198871:android:2b3a80736ff2e8d9f19492',datasets:ids.map(id=>({id,creationTime:String(created),transferConfig:'projects/514832198871/locations/asia-south1/transferConfigs/'+id,dataSourceId:sources[id]}))};
 const reader=async(id,kind)=>kind==='metadata'?{datasetReference:{projectId:'goatos-stg',datasetId:id},location:'asia-south1',creationTime:String(created)}:kind==='transfer'?{name:receipt.datasets.find(d=>d.id===id).transferConfig,destinationDatasetId:id,dataSourceId:sources[id],params:{platform:'ANDROID',client_namespace:receipt.appId,gmp_app_id:receipt.firebaseAppId}}:{totalItems:0};
 const pending=await verifyFirebaseInitialExport(receipt,'goatos-stg',reader,now);
 assert.equal(Object.keys(pending).length,2);
 const mobile=dashboard('05-mobile');
 for(const id of [1,5,9]){const panel=mobile.panels.find(p=>p.id===id);assert.match(firebasePendingReason(mobile.uid,panel,panel.targets[0],pending),/awaiting initial/);}
 const panel=mobile.panels.find(p=>p.id===1),query=panel.targets[0];
 assert.equal(firebasePendingReason('other',panel,query,pending),null);
 assert.equal(firebasePendingReason(mobile.uid,{...panel,id:4},query,pending),null);
 assert.throws(()=>firebasePendingReason(mobile.uid,panel,{...query,rawSql:query.rawSql+' AND false'},pending),/SQL changed/);
 assert.throws(()=>assertQueryResults({results:{A:{error:'database unavailable'}}},[query],'pending-provider'),/database unavailable/);
 assert.deepEqual(await verifyFirebaseInitialExport(receipt,'goatos-stg',reader,created+48*3600000),{});
 for(const changed of [{...receipt,project:'other'},{...receipt,appId:'other'},{...receipt,datasets:receipt.datasets.slice(1)},{...receipt,datasets:receipt.datasets.map(d=>({...d,creationTime:String(now+1)}))},{...receipt,datasets:receipt.datasets.map(d=>({...d,creationTime:String(now-1)}))}]) await assert.rejects(()=>verifyFirebaseInitialExport(changed,'goatos-stg',reader,now));
 await assert.rejects(()=>verifyFirebaseInitialExport(receipt,'goatos-stg',async()=>{throw Error('permission denied')},now),/permission denied/);
 for(const totalItems of [null,'',-1,0.5,undefined])await assert.rejects(()=>verifyFirebaseInitialExport(receipt,'goatos-stg',async(id,kind)=>kind==='tables'?{totalItems}:reader(id,kind),now),/Invalid Firebase table-list/);
 for(const change of [{disabled:true},{destinationDatasetId:'other'},{dataSourceId:'other'},{state:'FAILED'},{params:{platform:'ANDROID',client_namespace:'other',gmp_app_id:receipt.firebaseAppId}}])await assert.rejects(()=>verifyFirebaseInitialExport(receipt,'goatos-stg',async(id,kind)=>kind==='transfer'?{...await reader(id,kind),...change}:reader(id,kind),now),/disabled or source/);
 const arrived=await verifyFirebaseInitialExport(receipt,'goatos-stg',async(id,kind)=>kind==='tables'&&id==='firebase_performance'?{totalItems:1,tables:[{}]}:reader(id,kind),now);
 assert.equal(arrived.performance,undefined);assert.ok(arrived['crash-sessions']);
 const partial=await verifyFirebaseInitialExport(receipt,'goatos-stg',async(id,kind)=>kind==='tables'&&id==='firebase_sessions'?{totalItems:1,tables:[{}]}:reader(id,kind),now);
 assert.equal(partial['crash-sessions'],undefined);
});

test('query-validity deployment cannot masquerade as full data certification', async()=>{
 const {assertSmokeDataMode,assertExceptionMetricDefinition}=await import('./smoke-stg-grafana-dashboards.mjs');
 assert.match(assertSmokeDataMode({queryValidityOnly:true,empty:['RUM missing'],providerPending:[]}),/FINAL DATA CERTIFICATION PENDING/);
 assert.throws(()=>assertSmokeDataMode({queryValidityOnly:true,requireAll:true,empty:[],providerPending:[]}),/cannot be combined/);
 assert.throws(()=>assertSmokeDataMode({queryValidityOnly:false,empty:['RUM missing'],providerPending:['firebase']}),/targets are empty/);
 assert.match(assertSmokeDataMode({empty:[],providerPending:['firebase']}),/FIREBASE INITIAL EXPORT PENDING/);
 assert.equal(assertSmokeDataMode({empty:[],providerPending:[]}), 'FULL-DATA PASS');
 const metrics=JSON.parse(readFileSync(new URL('../../infra/observability/faro-log-metrics.json',import.meta.url)));
 const metric=metrics.find(m=>m.name==='goatos_rum_exceptions');
 assert.doesNotThrow(()=>assertExceptionMetricDefinition(metric,metric));
 for(const bad of [{},{...metric,disabled:true},{...metric,filter:'false'},{...metric,metricDescriptor:{...metric.metricDescriptor,valueType:'DISTRIBUTION'}}])assert.throws(()=>assertExceptionMetricDefinition(bad,metric),/missing or definition mismatched/);
});

test('actual all-panel HTTP sweep isolates provider empties and propagates query errors', async()=>{
 const {createServer}=await import('node:http');
 const {assertAllDashboardQueries}=await import('./smoke-stg-grafana-dashboards.mjs');
 let error=false;
 const server=createServer((req,res)=>{let body='';req.on('data',chunk=>body+=chunk);req.on('end',()=>{const q=JSON.parse(body).queries;res.setHeader('Content-Type','application/json');res.end(JSON.stringify({results:Object.fromEntries(q.map(t=>[t.refId,error?{error:'real database failure',status:500}:{status:200,frames:[]}]))}));});});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 try{
  const doc=dashboard('05-mobile');
  const panels=[doc.panels.find(p=>p.id===1),doc.panels.find(p=>p.id===4)];
  const dashboards=[{uid:doc.uid,doc:{...doc,panels,templating:{list:[]}}}];
  const args=[`http://127.0.0.1:${server.address().port}`,'test-only',2000,null,'goatos-stg',dashboards,{performance:'2026-09-18T09:09:59.642Z'}];
  const result=await assertAllDashboardQueries(...args);
  assert.equal(result.providerPending.length,1);assert.equal(result.empty.length,1);
  error=true;await assert.rejects(()=>assertAllDashboardQueries(...args),/real database failure/);
 }finally{await new Promise(resolve=>server.close(resolve));}
});


test('rollup omission is provider-specific and bound to exact receipt tables', async () => {
 const {firebaseRollupArgs} = await import('./smoke-stg-grafana-dashboards.mjs');
 const tables={crash:'goatos-stg.firebase_crashlytics.sg_mesha_goatos_ANDROID',sessions:'goatos-stg.firebase_sessions.sg_mesha_goatos_ANDROID',performance:'goatos-stg.firebase_performance.sg_mesha_goatos_ANDROID'};
 assert.equal(firebaseRollupArgs({},tables),'');
 assert.equal(firebaseRollupArgs({performance:'deadline'},tables),'-performance-bq-table=');
 assert.equal(firebaseRollupArgs({'crash-sessions':'deadline'},tables),'-crashlytics-bq-table=,-crashlytics-sessions-table=');
 assert.throws(()=>firebaseRollupArgs({performance:'deadline'},{...tables,performance:'other.dataset.table'}),/does not cover/);
 assert.throws(()=>firebaseRollupArgs({'crash-sessions':'deadline'},{...tables,sessions:'other.dataset.table'}),/does not cover/);
});
