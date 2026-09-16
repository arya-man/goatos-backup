import copy
import importlib.util
import pathlib
import tempfile
import shutil
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('observability', pathlib.Path(__file__).with_name('stg-observability.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def fixture():
    env = [{'name':k,'value':v} for k,v in {'GF_AUTH_GENERIC_OAUTH_ENABLED':'true','GF_AUTH_ANONYMOUS_ENABLED':'false','GF_SERVER_ROOT_URL':'https://grafana.mesha.sg/','OTHER':'keep'}.items()]
    env += [{'name':k,'valueFrom':{'secretKeyRef':{'name':k,'key':'latest'}}} for k in ['GF_AUTH_GENERIC_OAUTH_CLIENT_ID','GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET','GF_SECURITY_ADMIN_PASSWORD']]
    return {'metadata':{'annotations':{'run.googleapis.com/ingress':'internal-and-cloud-load-balancing'}},'spec':{'template':{'metadata':{'annotations':{'run.googleapis.com/cloudsql-instances':'goatos-stg:asia-south1:goatos-stg-core-db'}},'spec':{'containers':[{'image':'grafana/grafana:11.3.0','env':env}],'volumes':[{'name':'keep'}]}}}}


class Tests(unittest.TestCase):
    def test_secret_dependency_failure_prevents_all_mutations(self):
        root=pathlib.Path(__file__).resolve().parents[2]
        argv=['observability','deploy','--assets',str(root/'infra/grafana'),'--alloy-image','asia-south1-docker.pkg.dev/goatos-stg/goatos/grafana-alloy@sha256:'+'a'*64,'--log-metrics',str(root/'infra/observability/faro-log-metrics.json')]
        with patch('sys.argv',argv),patch.object(m,'service',return_value=fixture()),patch.object(m,'preflight_postgres_secret',side_effect=RuntimeError('missing')),patch.object(m,'upsert_metrics') as metrics,patch.object(m,'run') as run,patch.object(m,'update_service') as update:
            with self.assertRaises(RuntimeError):m.main()
            metrics.assert_not_called();run.assert_not_called();update.assert_not_called()

    def test_missing_secret_or_accessor_fails_preflight(self):
        doc=fixture()
        doc['spec']['template']['spec']['serviceAccountName']='grafana@goatos-stg.iam.gserviceaccount.com'
        with patch.object(m,'run',side_effect=RuntimeError('not found')):
            with self.assertRaises(RuntimeError): m.preflight_postgres_secret(doc)
        with patch.object(m,'run',side_effect=['{"state":"ENABLED"}','{"bindings":[]}']):
            with self.assertRaises(AssertionError): m.preflight_postgres_secret(doc)
        policy='{"bindings":[{"role":"roles/secretmanager.secretAccessor","members":["serviceAccount:grafana@goatos-stg.iam.gserviceaccount.com"]}]}'
        with patch.object(m,'run',side_effect=['{"state":"ENABLED"}',policy]):
            m.preflight_postgres_secret(doc)

    def test_pg_update_preserves_sso(self):
        before=fixture(); after=copy.deepcopy(before)
        c=m.container(after,'grafana')
        c['env'] += [{'name':k,'value':v} for k,v in m.PG_ENV.items()]
        c['env'].append({'name':'GOATOS_GRAFANA_POSTGRES_PASSWORD','valueFrom':{'secretKeyRef':{'name':m.PG_SECRET,'key':'latest'}}})
        m.verify_preserved(before,after,'grafana')
        c['env'][0]['value']='false'
        with self.assertRaises(AssertionError):m.verify_preserved(before,after,'grafana')

    def test_no_sidecar_or_volume_changes(self):
        b=fixture(); a=copy.deepcopy(b); image='new'
        m.container(a,'grafana-alloy')['image']=image
        m.verify_preserved(b,a,'grafana-alloy',image)
        a['spec']['template']['spec']['volumes']=[]
        with self.assertRaises(AssertionError):m.verify_preserved(b,a,'grafana-alloy',image)

    def test_auth_preflight_fail_closed(self):
        d=fixture();m.preflight_grafana(d)
        d['metadata']['annotations']['run.googleapis.com/ingress']='all'
        with self.assertRaises(AssertionError):m.preflight_grafana(d)

    def test_digest_and_project_required(self):
        good='asia-south1-docker.pkg.dev/goatos-stg/goatos/grafana-alloy@sha256:'+'a'*64
        m.validate_image(good)
        for bad in [good.replace('goatos-stg','goatos-prod'),good.replace('@sha256:'+ 'a'*64,':latest')]:
            with self.assertRaises(ValueError):m.validate_image(bad)

    def test_update_checks_ready_and_preservation_before_traffic(self):
        b=fixture();a=copy.deepcopy(b)
        c=m.container(a,'grafana')
        c['env'] += [{'name':k,'value':v} for k,v in m.PG_ENV.items()]
        c['env'].append({'name':'GOATOS_GRAFANA_POSTGRES_PASSWORD','valueFrom':{'secretKeyRef':{'name':m.PG_SECRET,'key':'latest'}}})
        a['status']={'latestCreatedRevisionName':'grafana-new'}
        for drift, ready in [(True,True),(False,False)]:
            candidate=copy.deepcopy(a)
            if drift:m.container(candidate,'grafana')['env'][0]['value']='false'
            calls=[]
            def fake(*args):
                calls.append(args)
                return m.json.dumps({'status':{'conditions':[{'type':'Ready','status':'True' if ready else 'False'}]}})
            with patch.object(m,'service',side_effect=[b,candidate]),patch.object(m,'run',side_effect=fake):
                with self.assertRaises(AssertionError):m.update_service('goatos-stg-grafana')
            self.assertFalse(any('update-traffic' in call for call in calls))

    def test_config_hash_changes_force_revision_and_readback(self):
        root=pathlib.Path(__file__).resolve().parents[2]/'infra/grafana'
        with tempfile.TemporaryDirectory() as tmp:
            shutil.copytree(root,tmp,dirs_exist_ok=True)
            digest=lambda: m.hashlib.sha256(m.json.dumps(m.assets(tmp),sort_keys=True).encode()).hexdigest()
            old=digest()
            with open(pathlib.Path(tmp)/'provisioning/datasources/datasources.yaml','a') as f:f.write('\n# changed\n')
            new=digest();self.assertNotEqual(old,new)
        b=fixture();a=copy.deepcopy(b);c=m.container(a,'grafana')
        c['env'] += [{'name':k,'value':v} for k,v in m.PG_ENV.items()]
        c['env'] += [{'name':'GOATOS_OBSERVABILITY_CONFIG_SHA','value':new},{'name':'GOATOS_GRAFANA_POSTGRES_PASSWORD','valueFrom':{'secretKeyRef':{'name':m.PG_SECRET,'key':'latest'}}}]
        a['status']={'latestCreatedRevisionName':'grafana-new','traffic':[{'revisionName':'grafana-new','percent':100}]}
        calls=[]
        def fake(*args):
            calls.append(args)
            return m.json.dumps({'status':{'conditions':[{'type':'Ready','status':'True'}]}})
        with patch.object(m,'service',side_effect=[b,a,a]),patch.object(m,'run',side_effect=fake):
            m.update_service('goatos-stg-grafana',config_sha=new)
        self.assertTrue(any('GOATOS_OBSERVABILITY_CONFIG_SHA='+new in arg for call in calls for arg in call))
        with self.assertRaises(AssertionError):m.verify_preserved(b,a,'grafana',config_sha=old)

    def test_log_metric_scope_readback_and_immutable_type(self):
        path=pathlib.Path(__file__).resolve().parents[2]/'infra/observability/faro-log-metrics.json'
        metrics=m.load_metrics(path)
        actual=copy.deepcopy(metrics[0]);actual['metricDescriptor']['name']='server-added'
        m.verify_metric(metrics[0],actual)
        actual['filter']='wrong'
        with self.assertRaises(AssertionError):m.verify_metric(metrics[0],actual)
        old=copy.deepcopy(metrics[0]);old['metricDescriptor']['valueType']='INT64'
        calls=[]
        def fake(*args):
            calls.append(args);return m.json.dumps([old])
        with patch.object(m,'run',side_effect=fake):
            with self.assertRaises(AssertionError):m.upsert_metrics(metrics)
        self.assertEqual(len(calls),1) # reject before any upsert

    def test_real_asset_manifest_bounded(self):
        root=pathlib.Path(__file__).resolve().parents[2]/'infra/grafana'
        manifest=m.assets(root)
        self.assertEqual(len(manifest),9)
        self.assertEqual(sum(p.endswith('.json') for p in manifest),7)

if __name__=='__main__':unittest.main()
