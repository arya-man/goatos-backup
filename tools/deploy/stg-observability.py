#!/usr/bin/env python3
"""Cloud Deploy-only bounded observability updater; never reads secret values."""
import argparse
import copy
import hashlib
import json
import pathlib
import re
import subprocess
import tempfile

PROJECT = 'goatos-stg'
REGION = 'asia-south1'
BUCKET = 'gs://goatos-stg-grafana-provisioning/'
PG_ENV = {
    'GOATOS_GRAFANA_POSTGRES_SOCKET': '/cloudsql/goatos-stg:asia-south1:goatos-stg-core-db',
    'GOATOS_GRAFANA_POSTGRES_USER': 'goatos_grafana_ro',
}
PG_SECRET = 'goatos-stg-grafana-postgres-datasource-password'


def run(*args):
    return subprocess.check_output(args, text=True)


def service(name):
    return json.loads(run('gcloud', 'run', 'services', 'describe', name, '--project='+PROJECT, '--region='+REGION, '--format=json'))


def container(doc, name):
    cs = doc['spec']['template']['spec']['containers']
    found = [c for c in cs if c.get('name') == name]
    if len(found) == 1:
        return found[0]
    if len(cs) == 1 and not cs[0].get('name'):
        return cs[0]
    raise ValueError('Ambiguous container: '+name)


def assets(root):
    root = pathlib.Path(root)
    files = sorted((root/'dashboards').glob('*.json'))
    if len(files) != 7:
        raise ValueError('Exactly seven dashboards required')
    uids = [json.loads(p.read_text())['uid'] for p in files]
    if len(set(uids)) != 7:
        raise ValueError('Duplicate dashboard UID')
    files += [root/'provisioning/dashboards/dashboards.yaml', root/'provisioning/datasources/datasources.yaml']
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}


def validate_image(image):
    if not re.fullmatch(r'asia-south1-docker\.pkg\.dev/goatos-stg/goatos/grafana-alloy@sha256:[a-f0-9]{64}', image):
        raise ValueError('Alloy image must be an immutable staging digest')


def verify_preserved(before, after, name, image=None, config_sha=None):
    b, a = copy.deepcopy(before['spec']['template']['spec']), copy.deepcopy(after['spec']['template']['spec'])
    bc, ac = container({'spec': {'template': {'spec': b}}}, name), container({'spec': {'template': {'spec': a}}}, name)
    if image:
        assert ac['image'] == image, 'Alloy image readback mismatch'
        ac['image'] = bc['image']
    else:
        env = {e['name']: e for e in ac.get('env', [])}
        if config_sha:
            assert env.get('GOATOS_OBSERVABILITY_CONFIG_SHA', {}).get('value') == config_sha, 'Config SHA mismatch'
        for key, val in PG_ENV.items():
            assert env.get(key, {}).get('value') == val, 'Postgres environment mismatch'
        assert env.get('GOATOS_GRAFANA_POSTGRES_PASSWORD', {}).get('valueFrom', {}).get('secretKeyRef') == {'name': PG_SECRET, 'key': 'latest'}, 'Postgres secret reference mismatch'
        for c in (bc, ac):
            c['env'] = sorted([e for e in c.get('env', []) if e['name'] not in {*PG_ENV, 'GOATOS_GRAFANA_POSTGRES_PASSWORD', 'GOATOS_OBSERVABILITY_CONFIG_SHA'}], key=lambda e:e['name'])
    assert b == a, 'Unrelated runtime configuration changed (including OAuth)'
    assert before['metadata'].get('annotations', {}).get('run.googleapis.com/ingress') == after['metadata'].get('annotations', {}).get('run.googleapis.com/ingress'), 'Ingress changed'


def preflight_grafana(doc):
    c = container(doc, 'grafana')
    env = {e['name']: e for e in c.get('env', [])}
    for key, expected in {'GF_AUTH_GENERIC_OAUTH_ENABLED':'true', 'GF_AUTH_ANONYMOUS_ENABLED':'false', 'GF_SERVER_ROOT_URL':'https://grafana.mesha.sg/'}.items():
        assert env.get(key, {}).get('value') == expected, 'Unsafe Grafana auth configuration: '+key
    for key in ('GF_AUTH_GENERIC_OAUTH_CLIENT_ID','GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET','GF_SECURITY_ADMIN_PASSWORD'):
        assert env.get(key, {}).get('valueFrom', {}).get('secretKeyRef'), 'Missing secret reference: '+key
    assert doc['metadata']['annotations'].get('run.googleapis.com/ingress') == 'internal-and-cloud-load-balancing', 'Grafana must remain LB-only'
    assert doc['spec']['template']['metadata']['annotations'].get('run.googleapis.com/cloudsql-instances') == 'goatos-stg:asia-south1:goatos-stg-core-db', 'Cloud SQL attachment missing'


def preflight_postgres_secret(doc):
    # Metadata only: never access the password value during deployment.
    version = json.loads(run('gcloud', 'secrets', 'versions', 'describe', 'latest', '--secret='+PG_SECRET, '--project='+PROJECT, '--format=json'))
    assert version.get('state') == 'ENABLED', 'Grafana Postgres secret has no enabled latest version; run readonly bootstrap first'
    sa = doc['spec']['template']['spec'].get('serviceAccountName')
    assert sa, 'Grafana runtime service account missing'
    policy = json.loads(run('gcloud', 'secrets', 'get-iam-policy', PG_SECRET, '--project='+PROJECT, '--format=json'))
    assert any(b.get('role') == 'roles/secretmanager.secretAccessor' and 'serviceAccount:'+sa in b.get('members', []) and not b.get('condition') for b in policy.get('bindings', [])), 'Grafana runtime requires explicit unconditional secret accessor grant'


def update_service(name, image=None, config_sha=None):
    before = service(name)
    cname = 'grafana-alloy' if image else 'grafana'
    c = container(before, cname)
    args = ['gcloud', 'run', 'services', 'update', name, '--project='+PROJECT, '--region='+REGION, '--no-traffic', '--quiet']
    if c.get('name'):
        args.append('--container='+c['name'])
    if image:
        validate_image(image)
        args.append('--image='+image)
    else:
        env = {e['name']: e for e in c.get('env', [])}
        assert env.get('GF_AUTH_GENERIC_OAUTH_ENABLED', {}).get('value') == 'true', 'SSO missing before update'
        assert before['spec']['template']['metadata']['annotations'].get('run.googleapis.com/cloudsql-instances') == 'goatos-stg:asia-south1:goatos-stg-core-db', 'Cloud SQL attachment missing'
        changes = dict(PG_ENV)
        if config_sha:
            changes['GOATOS_OBSERVABILITY_CONFIG_SHA'] = config_sha
        args += ['--update-env-vars='+','.join(k+'='+v for k,v in changes.items()), '--update-secrets=GOATOS_GRAFANA_POSTGRES_PASSWORD='+PG_SECRET+':latest']
    run(*args)
    after = service(name)
    verify_preserved(before, after, cname, image, config_sha)
    revision = after['status']['latestCreatedRevisionName']
    rev = json.loads(run('gcloud','run','revisions','describe',revision,'--project='+PROJECT,'--region='+REGION,'--format=json'))
    assert any(c.get('type') == 'Ready' and c.get('status') == 'True' for c in rev['status']['conditions']), 'Revision not Ready'
    run('gcloud','run','services','update-traffic',name,'--project='+PROJECT,'--region='+REGION,'--to-revisions='+revision+'=100','--quiet')
    live = service(name)
    assert any(t.get('revisionName') == revision and t.get('percent') == 100 for t in live['status']['traffic']), 'Traffic readback mismatch'
    return revision


METRIC_NAMES = {'goatos_rum_lcp_ms','goatos_rum_inp_ms','goatos_rum_ttfb_ms','goatos_rum_cls','goatos_rum_exceptions','goatos_rum_route_views','goatos_rum_session_starts'}

def load_metrics(path):
    metrics = json.loads(pathlib.Path(path).read_text())
    assert len(metrics) == 7 and {m['name'] for m in metrics} == METRIC_NAMES, 'Unexpected logging metric set'
    for metric in metrics:
        for scope in ['logName="projects/goatos-stg/logs/goatos-stg-faro"','jsonPayload.app_name="mesha-admin-web"','jsonPayload.app_environment="stg"']:
            assert scope in metric['filter'], 'Metric filter missing scope'
        descriptor=metric['metricDescriptor']
        assert descriptor['metricKind']=='DELTA' and descriptor['valueType'] in ('INT64','DISTRIBUTION'), 'Unexpected metric type'
        if descriptor['valueType']=='DISTRIBUTION':
            assert metric.get('valueExtractor') and metric.get('bucketOptions'), 'Distribution definition incomplete'
    return metrics


def verify_metric(expected, actual):
    # Google adds resource names and timestamps to descriptors; compare every
    # declared semantic field recursively, without discarding requested labels.
    def subset(e,a):
        if isinstance(e,dict):
            return isinstance(a,dict) and all(k in a and subset(v,a[k]) for k,v in e.items())
        return e==a
    assert subset(expected,actual), 'Logging metric readback mismatch: '+expected['name']
    assert not actual.get('disabled',False), 'Logging metric disabled'


def upsert_metrics(metrics):
    existing=json.loads(run('gcloud','logging','metrics','list','--project='+PROJECT,'--format=json'))
    names={m['name'] for m in existing}
    by_name={m['name']:m for m in existing}
    # Never attempt an incompatible descriptor update or delete/recreate metrics.
    for metric in metrics:
        if metric['name'] in by_name:
            old=by_name[metric['name']].get('metricDescriptor',{})
            for field in ('metricKind','valueType'):
                assert old.get(field)==metric['metricDescriptor'][field], 'Immutable metric type mismatch: '+metric['name']
    with tempfile.TemporaryDirectory(prefix='goatos-log-metrics-') as tmp:
        for metric in metrics:
            path=pathlib.Path(tmp)/(metric['name']+'.json')
            path.write_text(json.dumps(metric))
            action='update' if metric['name'] in names else 'create'
            run('gcloud','logging','metrics',action,metric['name'],'--project='+PROJECT,'--config-from-file='+str(path),'--quiet')
            actual=json.loads(run('gcloud','logging','metrics','describe',metric['name'],'--project='+PROJECT,'--format=json'))
            verify_metric(metric,actual)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=['plan','deploy'])
    parser.add_argument('--assets', required=True)
    parser.add_argument('--alloy-image', required=True)
    parser.add_argument('--log-metrics', required=True)
    args = parser.parse_args()
    validate_image(args.alloy_image)
    manifest = assets(args.assets)
    metrics = load_metrics(args.log_metrics)
    if args.command == 'plan':
        print(json.dumps({'objects':manifest,'alloy_image':args.alloy_image,'log_metrics':metrics}, sort_keys=True))
        return
    grafana = service('goatos-stg-grafana')
    preflight_grafana(grafana)
    preflight_postgres_secret(grafana)
    container(service('goatos-stg-grafana-alloy'), 'grafana-alloy')
    upsert_metrics(metrics)
    # All input validation precedes mutation. Fixed object names only, no rsync/delete.
    for path, digest in manifest.items():
        run('gcloud','storage','cp',str(pathlib.Path(args.assets)/path),BUCKET+path)
        actual = subprocess.check_output(['gcloud','storage','cat',BUCKET+path])
        assert hashlib.sha256(actual).hexdigest() == digest, 'Provisioning object readback mismatch: '+path
    config_sha = hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest()
    grafana_revision = update_service('goatos-stg-grafana', config_sha=config_sha)
    alloy_revision = update_service('goatos-stg-grafana-alloy', args.alloy_image)
    print(json.dumps({'objects':manifest,'grafana_revision':grafana_revision,'alloy_revision':alloy_revision},sort_keys=True))

if __name__ == '__main__':
    main()
