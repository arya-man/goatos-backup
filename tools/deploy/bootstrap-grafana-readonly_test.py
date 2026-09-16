import importlib.util
import pathlib
import unittest
import json
import io
import os
import secrets
from urllib.parse import quote
from contextlib import redirect_stdout
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('bootstrap', pathlib.Path(__file__).with_name('bootstrap-grafana-readonly.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class BootstrapTests(unittest.TestCase):
    def test_deployer_only_receives_secret_scoped_metadata(self):
        with patch.object(m,'invoke') as invoke:
            m.grant_secret_metadata('runtime@goatos-stg.iam.gserviceaccount.com')
            calls=[c.args[0] for c in invoke.call_args_list]
            self.assertEqual(len(calls),2)
            self.assertIn('--role=roles/secretmanager.secretAccessor',calls[0])
            self.assertIn('--role=roles/secretmanager.viewer',calls[1])
            self.assertIn('--member=serviceAccount:'+m.DEPLOYER,calls[1])
            self.assertIn(m.SECRET,calls[1])
            self.assertNotIn('--role=roles/secretmanager.secretAccessor',calls[1])

    def test_uri_fields_and_query_host_without_inherited_pg_overrides(self):
        # Generated parser-only input; reserved characters must round-trip through URI escaping.
        fixture_password=secrets.token_hex(12)+'@:/?+#%'
        uri='postgresql://user:'+quote(fixture_password,safe='')+'@localhost:15479/goatos?host=%2Fcloudsql%2Finstance&sslmode=disable'
        env=m.connection_env({'DATABASE_URL':uri,'PGHOST':'wrong','PGSERVICE':'unexpected','PATH':'keep'})
        self.assertEqual(env['PGHOST'],'/cloudsql/instance')
        self.assertEqual(env['PGPORT'],'15479')
        self.assertEqual(env['PGPASSWORD'],fixture_password)
        self.assertEqual(env['PGDATABASE'],'goatos')
        self.assertEqual(env['PGSSLMODE'],'disable')
        self.assertNotIn('PGSERVICE',env)
        self.assertNotIn('DATABASE_URL',env)
    @unittest.skipUnless(os.environ.get('GOATOS_BOOTSTRAP_TEST_DSN'), 'explicit local throwaway database required')
    def test_actual_postgres_connection(self):
        with patch.dict(m.os.environ,{'DATABASE_URL':os.environ['GOATOS_BOOTSTRAP_TEST_DSN'],'PGHOST':'invalid-inherited-host','PGPORT':'1'}):
            self.assertEqual(m.psql('SELECT 41+1;').strip(),'42')

    def test_existing_role_or_secret_prevents_mutations(self):
        doc=json.dumps({'spec':{'template':{'spec':{'serviceAccountName':'grafana@goatos-stg.iam.gserviceaccount.com'}}}})
        for facts,existing in [('goatos|t|t',[]),('goatos|t|f',[{'name':'projects/goatos-stg/secrets/'+m.SECRET}])]:
            with patch('sys.argv',['bootstrap','--apply']), patch.dict(m.os.environ,{'DATABASE_URL':'private'}), patch.object(m,'psql',return_value=facts) as pg, patch.object(m,'invoke',side_effect=[doc,json.dumps(existing)]) as invoke, redirect_stdout(io.StringIO()):
                with self.assertRaises(RuntimeError):m.main()
                self.assertEqual(pg.call_count,1)
                self.assertTrue(all('create' not in call.args[0] and 'add' not in call.args[0] for call in invoke.call_args_list))
    def test_partial_secret_failure_stops_without_retry_or_printing_password(self):
        doc=json.dumps({'spec':{'template':{'spec':{'serviceAccountName':'grafana@goatos-stg.iam.gserviceaccount.com'}}}})
        output=io.StringIO()
        with patch('sys.argv',['bootstrap','--apply']),patch.dict(m.os.environ,{'DATABASE_URL':'private'}),patch.object(m,'psql',side_effect=['goatos|t|f','engagement_daily','']) as pg,patch.object(m.secrets,'token_hex',return_value='a'*64),patch.object(m,'invoke',side_effect=[doc,'[]','',RuntimeError('version failed')]) as invoke,redirect_stdout(output):
            with self.assertRaisesRegex(RuntimeError,'version failed'):m.main()
            self.assertEqual(pg.call_count,3)
            self.assertEqual(invoke.call_count,4)
            self.assertNotIn('a'*64,output.getvalue())

    def test_grants_only_explicit_existing_summary_tables(self):
        sql = m.sql('a'*64, ['engagement_daily', 'rollup_run'])
        self.assertIn('GRANT SELECT ON analytics.engagement_daily,analytics.rollup_run', sql)
        self.assertNotIn('ALL TABLES', sql)
        self.assertNotIn('DEFAULT PRIVILEGES', sql)
        self.assertIn('NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT', sql)
        self.assertIn("has_table_privilege('goatos_grafana_ro','analytics.app_events','SELECT')", sql)
        self.assertNotIn('analytics.app_crash_daily',sql)
    def test_rejects_raw_event_grant_and_bad_password(self):
        with self.assertRaises(ValueError): m.sql('a'*64,['app_events'])
        with self.assertRaises(ValueError): m.sql("unsafe'password")
    def test_subprocess_failure_cannot_expose_sql_or_password(self):
        with patch.object(m.subprocess,'run') as run:
            run.return_value.returncode=1
            run.return_value.stderr='SECRET PASSWORD SQL'
            with self.assertRaisesRegex(RuntimeError,'details suppressed') as error:
                m.invoke(['psql'], 'SECRET PASSWORD SQL')
            self.assertNotIn('SECRET',str(error.exception))

if __name__=='__main__': unittest.main()
