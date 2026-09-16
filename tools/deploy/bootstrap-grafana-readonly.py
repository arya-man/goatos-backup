#!/usr/bin/env python3
"""Explicit one-time STG Grafana summary-only database credential bootstrap.

Default is a nonmutating plan. --apply requires DATABASE_URL for the existing
admin connection (normally via the documented Cloud SQL Auth Proxy). Passwords
never appear in argv, output, files, or exceptions. Existing role/secret refuse
initialization; partial failure requires inspection, never blind rotation.
"""
import argparse
import json
import os
import secrets
import subprocess
from urllib.parse import urlsplit, unquote, parse_qsl

PROJECT = 'goatos-stg'
ROLE = 'goatos_grafana_ro'
SECRET = 'goatos-stg-grafana-postgres-datasource-password'
DEPLOYER = 'goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com'
TABLES = ('engagement_daily', 'funnel_daily', 'journey_daily', 'crash_daily',
          'rollup_run', 'app_crash_daily', 'app_network_daily')


def sql(password, existing=TABLES):
    if len(password) != 64 or any(c not in '0123456789abcdef' for c in password):
        raise ValueError('Invalid generated credential')
    if not existing or any(t not in TABLES for t in existing):
        raise ValueError('Only enumerated existing summary tables may be granted')
    tables = ','.join('analytics.'+t for t in existing)
    return ("BEGIN;\nCREATE ROLE "+ROLE+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION PASSWORD '"+password+"';\n"
            "GRANT CONNECT ON DATABASE goatos TO "+ROLE+";\n"
            "GRANT USAGE ON SCHEMA analytics TO "+ROLE+";\n"
            "GRANT SELECT ON "+tables+" TO "+ROLE+";\n"
            "DO $$ BEGIN IF has_table_privilege('goatos_grafana_ro','analytics.app_events','SELECT') THEN RAISE EXCEPTION 'Unexpected public raw-event access'; END IF; END $$;\nCOMMIT;\n")


def invoke(args, data=None, env=None):
    result = subprocess.run(args, input=data, text=True, capture_output=True, env=env)
    if result.returncode:
        # psql can echo failed SQL including credentials; never relay stderr.
        raise RuntimeError('Bootstrap operation failed: '+args[0]+' (details suppressed)')
    return result.stdout


def connection_env(source):
    # libpq does not parse a URI supplied via PGDATABASE. Split it explicitly,
    # keeping credentials out of argv and preventing inherited PG* overrides.
    url = urlsplit(source.get('DATABASE_URL', ''))
    if url.scheme not in ('postgres', 'postgresql') or not url.username or not url.path.strip('/'):
        raise ValueError('DATABASE_URL must be a PostgreSQL URI with user and database')
    env = {k:v for k,v in source.items() if not k.startswith('PG') and k != 'DATABASE_URL'}
    values = {'user':unquote(url.username), 'password':unquote(url.password or ''),
              'host':url.hostname or '', 'port':str(url.port or 5432),
              'dbname':unquote(url.path[1:])}
    mapping = {'user':'PGUSER','password':'PGPASSWORD','host':'PGHOST',
               'port':'PGPORT','dbname':'PGDATABASE','sslmode':'PGSSLMODE',
               'sslrootcert':'PGSSLROOTCERT','sslcert':'PGSSLCERT',
               'sslkey':'PGSSLKEY','connect_timeout':'PGCONNECT_TIMEOUT'}
    seen = set()
    for key,value in parse_qsl(url.query, keep_blank_values=True):
        if key not in mapping or key in seen:
            raise ValueError('Unsupported or duplicate DATABASE_URL connection option')
        seen.add(key)
        values[key] = value
    if not values['host'] or not values['user'] or not values['dbname']:
        raise ValueError('Explicit PostgreSQL host, user and database required')
    env.update({mapping[k]:v for k,v in values.items()})
    return env


def psql(query):
    env = connection_env(os.environ)

    return invoke(['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At'], query, env)


def grant_secret_metadata(runtime_sa):
    for principal, role in ((runtime_sa,'roles/secretmanager.secretAccessor'),
                            (DEPLOYER,'roles/secretmanager.viewer')):
        invoke(['gcloud','secrets','add-iam-policy-binding',SECRET,'--project='+PROJECT,
                '--member=serviceAccount:'+principal,'--role='+role,'--quiet'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    doc = json.loads(invoke(['gcloud','run','services','describe','goatos-stg-grafana','--project='+PROJECT,'--region=asia-south1','--format=json']))
    sa = doc['spec']['template']['spec']['serviceAccountName']
    if not sa.endswith('@'+PROJECT+'.iam.gserviceaccount.com'):
        raise RuntimeError('Unexpected runtime service account')
    print(json.dumps({'project':PROJECT,'role':ROLE,'secret':SECRET,'runtime_service_account':sa,'select_tables':['analytics.'+t for t in TABLES],'mode':'apply' if args.apply else 'plan'}))
    if not args.apply:
        return
    if not os.environ.get('DATABASE_URL'):
        raise RuntimeError('DATABASE_URL admin connection required')
    # Confirm database target and administrative permission before any writes.
    facts = psql("SELECT current_database(), EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user AND (rolsuper OR rolcreaterole)), EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goatos_grafana_ro');")
    if facts.strip() != 'goatos|t|f':
        raise RuntimeError('Expected goatos database, admin role-create permission, and absent readonly role')
    existing = json.loads(invoke(['gcloud','secrets','list','--project='+PROJECT,'--format=json']))
    if any(s['name'].endswith('/'+SECRET) for s in existing):
        raise RuntimeError('Secret already exists; refuse credential rotation')
    password = secrets.token_hex(32)
    existing_tables = psql("SELECT tablename FROM pg_tables WHERE schemaname='analytics' ORDER BY tablename;").splitlines()
    selected = [t for t in TABLES if t in existing_tables]
    psql(sql(password, selected))
    invoke(['gcloud','secrets','create',SECRET,'--project='+PROJECT,'--replication-policy=automatic','--quiet'])
    invoke(['gcloud','secrets','versions','add',SECRET,'--project='+PROJECT,'--data-file=-'],password)
    password = None
    grant_secret_metadata(sa)
    print('Readonly role and dedicated secret initialized; verify summary queries as this role before deployment.')

if __name__ == '__main__':
    main()
