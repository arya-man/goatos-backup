import copy
import importlib.util
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('sso', __file__.replace('_test.py', '.py'))
sso = importlib.util.module_from_spec(spec); spec.loader.exec_module(sso)

def fixture():
    return {'metadata': {'annotations': {'run.googleapis.com/ingress': 'internal-and-cloud-load-balancing'}}, 'spec': {'template': {'spec': {'serviceAccountName': 'runtime', 'containers': [{'image': 'grafana/grafana:11.3.0', 'env': [
        {'name': 'GF_AUTH_ANONYMOUS_ENABLED', 'value': 'false'},
        {'name': 'GF_USERS_ALLOW_SIGN_UP', 'value': 'false'},
        {'name': 'GF_SERVER_ROOT_URL', 'value': 'https://grafana.mesha.sg/'},
        {'name': 'GF_SECURITY_ADMIN_PASSWORD', 'valueFrom': {'secretKeyRef': {'name': 'admin-password', 'key': 'latest'}}},
    ]}]}}}}

def configured(before):
    after = copy.deepcopy(before)
    c = sso.container(after)
    c['env'] += [{'name': k, 'value': v} for k, v in sso.ENV.items()]
    c['env'] += [{'name': k, 'valueFrom': {'secretKeyRef': {'name': v, 'key': 'latest'}}} for k, v in sso.SECRETS.items()]
    return after

class SsoTest(unittest.TestCase):
    def test_unnamed_and_named_runtime_preserved(self):
        for name in [None, 'grafana']:
            before = fixture()
            if name: sso.container(before)['name'] = name
            sso.verify(before, configured(before))
    def test_scope_and_secret_refs_in_actual_update(self):
        with patch.object(sso.subprocess, 'run') as run:
            sso.update(fixture())
        args = run.call_args.args[0]
        self.assertIn('--no-traffic', args)
        self.assertNotIn('--image', ' '.join(args))
        self.assertIn('--update-env-vars=^~^' + '~'.join(k+'='+v for k,v in sso.ENV.items()), args)
        self.assertIn('--update-secrets=' + ','.join(k+'='+v+':latest' for k,v in sso.SECRETS.items()), args)
    def test_readback_rejects_runtime_and_auth_drift(self):
        for mutate in [
            lambda a: sso.container(a).update(image='other'),
            lambda a: a['spec']['template']['spec'].update(serviceAccountName='other'),
            lambda a: a['metadata']['annotations'].update({'run.googleapis.com/ingress':'all'}),
            lambda a: sso.container(a)['env'].append({'name':'UNRELATED','value':'changed'}),
            lambda a: next(e for e in sso.container(a)['env'] if e['name']=='GF_AUTH_GENERIC_OAUTH_ROLE_ATTRIBUTE_STRICT').update(value='false'),
            lambda a: next(e for e in sso.container(a)['env'] if e['name']=='GF_USERS_ALLOW_SIGN_UP').update(value='true'),
            lambda a: next(e for e in sso.container(a)['env'] if e['name']=='GF_AUTH_ANONYMOUS_ENABLED').update(value='true'),
        ]:
            before=fixture();after=configured(before);mutate(after)
            with self.assertRaises(AssertionError): sso.verify(before,after)
    def test_auth_gate_configuration(self):
        self.assertEqual(sso.ENV['GF_AUTH_GENERIC_OAUTH_ROLE_ATTRIBUTE_STRICT'],'true')
        self.assertEqual(sso.ENV['GF_AUTH_GENERIC_OAUTH_SKIP_ORG_ROLE_SYNC'],'false')
        self.assertEqual(sso.ENV['GF_AUTH_GENERIC_OAUTH_ORG_MAPPING'],'')
        self.assertEqual(sso.ENV['GF_AUTH_GENERIC_OAUTH_ALLOW_ASSIGN_GRAFANA_ADMIN'],'false')
        self.assertEqual(sso.ENV['GF_AUTH_GENERIC_OAUTH_ALLOW_SIGN_UP'],'true')
        self.assertEqual(sso.ENV['GF_AUTH_BASIC_ENABLED'],'true')
    def test_redirect_boundary(self):
        header='Location: https://accounts.google.com/o/oauth2/v2/auth?redirect_uri=https%3A%2F%2Fgrafana.mesha.sg%2Flogin%2Fgeneric_oauth&state=state&client_id=client&code_challenge_method=S256&code_challenge=challenge\r\n'
        sso.verify_redirect(header)
        for bad in [header.replace('accounts.google.com','evil.test'),header.replace('grafana.mesha.sg','evil.test'),header.replace('state=state','state='),header.replace('S256','plain')]:
            with self.assertRaises(AssertionError):sso.verify_redirect(bad)

if __name__=='__main__':unittest.main()
