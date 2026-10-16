#!/usr/bin/env python3
"""Synthetic local-only PostgreSQL 17 upgrade rehearsal; never runs down/drop.

Baseline: python3 scripts/verify-upstream-v062.py baseline --server /path/to/base/server
Upgrade:  python3 scripts/verify-upstream-v062.py upgrade --server /path/to/candidate/server
Fresh:    python3 scripts/verify-upstream-v062.py fresh --server /path/to/candidate/server

All database commands target the dedicated container and its loopback port only.
Artifacts include complete synthetic snapshots and an actual pg_dump backup.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import tempfile
import uuid

CONTAINER = 'multica-v062-rehearsal-postgres'
ARTIFACTS = Path(tempfile.gettempdir()) / 'multica-v062-rehearsal'
TABLES = ['user', 'workspace', 'member', 'project', 'agent_runtime', 'agent', 'issue', 'goal', 'goal_issue', 'milestone', 'milestone_date_change', 'milestone_release', 'milestone_proposal', 'channel_installation']
STAMP = '2026-10-16T08:00:00Z'
TARGET_RELEASE_COMMIT = '7ab92d7622ecd81d179a8d83ae2dea74835a5069'


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def query(database, sql):
    return run(['docker', 'exec', '-i', CONTAINER, 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-qAt', '-U', 'multica', '-d', database], input=sql, text=True, capture_output=True).stdout.strip()


def quote(value):
    return '"' + value.replace('"', '""') + '"'


def literal(value):
    if value is None:
        return 'NULL'
    if isinstance(value, bool):
        return 'true' if value else 'false'
    if isinstance(value, (int, float)):
        return str(value)
    if isinstance(value, (dict, list)):
        value = json.dumps(value, sort_keys=True)
    return "'" + str(value).replace("'", "''") + "'"


def ident(name):
    return str(uuid.uuid5(uuid.NAMESPACE_URL, 'https://example.test/multica-v062-rehearsal/' + name))


def seed_sql():
    rows = []
    def add(table, **values):
        rows.append('INSERT INTO ' + quote(table) + ' (' + ','.join(map(quote, values)) + ') VALUES (' + ','.join(map(literal, values.values())) + ');')
    ws, user, project, runtime, agent = map(ident, ['workspace', 'user', 'project', 'runtime', 'agent'])
    add('user', id=user, name='Synthetic rehearsal sponsor', email='v062-rehearsal@example.test', created_at=STAMP, updated_at=STAMP)
    add('workspace', id=ws, name='Synthetic v062 rehearsal', slug='synthetic-v062-rehearsal', issue_prefix='RHR', issue_counter=1, created_at=STAMP, updated_at=STAMP)
    add('member', id=ident('member'), workspace_id=ws, user_id=user, role='owner', created_at=STAMP)
    add('project', id=project, workspace_id=ws, title='Synthetic adoption product', status='in_progress', lead_type='member', lead_id=user, created_at=STAMP, updated_at=STAMP)
    add('agent_runtime', id=runtime, workspace_id=ws, name='Synthetic offline runtime', runtime_mode='cloud', provider='rehearsal_fake_runtime', status='offline', owner_id=user, created_at=STAMP, updated_at=STAMP)
    for name in ['agent', 'revoked-agent']:
        add('agent', id=ident(name), workspace_id=ws, name='Synthetic ' + name, runtime_mode='cloud', runtime_id=runtime, owner_id=user, created_at=STAMP, updated_at=STAMP)
    issue = ident('issue')
    add('issue', id=issue, workspace_id=ws, title='Synthetic implementation issue', number=1, creator_type='member', creator_id=user, assignee_type='agent', assignee_id=agent, project_id=project, description='Fixture only; no customer information.', created_at=STAMP, updated_at=STAMP)
    for name, level, parent, previous, kind, status in [('direction',1,None,None,None,'in_progress'), ('product-goal',2,'direction',None,'base','in_progress'), ('previous-cycle',3,'product-goal',None,None,'done'), ('cycle',3,'product-goal','previous-cycle',None,'at_risk')]:
        add('goal', id=ident(name), workspace_id=ws, level=level, title='Synthetic ' + name, parent_goal_id=ident(parent) if parent else None, prev_goal_id=ident(previous) if previous else None, project_id=project, kind=kind, status=status, owner_type='agent' if level==3 else 'member', owner_id=agent if level==3 else user, cycle='2026-Q4', due_date='2026-12-31', created_by_type='member', created_by_id=user, created_at=STAMP, updated_at=STAMP)
    add('goal_issue', goal_id=ident('cycle'), issue_id=issue, workspace_id=ws, linked_by_type='agent', linked_by_id=agent, linked_at=STAMP)
    for name, typ, n, status, adoption in [('launch','launch',None,'achieved',None), ('first-use','first_use',None,'pending_accept','verifier'), ('nth-use','nth_use',10,'in_progress','analytics')]:
        add('milestone', id=ident(name), workspace_id=ws, goal_id=ident('cycle'), type=typ, n=n, title='Synthetic ' + name, value_statement='Synthetic adoption evidence remains intact.', original_planned_date='2026-10-01', planned_date='2026-10-15' if name=='nth-use' else '2026-10-01', actual_date='2026-10-01' if status=='achieved' else None, status=status, is_delayed=name=='nth-use', adoption_check=adoption, adoption_config={'event':'synthetic_used','threshold':10} if adoption=='analytics' else {}, verifier_type='external' if adoption=='verifier' else None, verifier_label='Synthetic external verifier' if adoption=='verifier' else '', accepted_by_type='member' if status=='achieved' else None, accepted_by_id=user if status=='achieved' else None, accepted_at=STAMP if status=='achieved' else None, created_by_type='member', created_by_id=user, created_at=STAMP, updated_at=STAMP)
    add('milestone_date_change', id=ident('date-change'), workspace_id=ws, milestone_id=ident('nth-use'), from_date='2026-10-01', to_date='2026-10-15', reason='Synthetic reschedule retained for migration verification.', changed_by_type='member', changed_by_id=user, created_at=STAMP)
    for name, kind in [('release','main'), ('hotfix','hotfix')]:
        add('milestone_release', id=ident(name), workspace_id=ws, milestone_id=ident('launch'), kind=kind, repo_url='https://example.test/synthetic/repository', ref='synthetic-ref', tag='synthetic-' + name, released_at='2026-10-01', summary='Synthetic release notes.', summary_by_type='agent', summary_by_id=agent, summary_at=STAMP, created_by_type='member', created_by_id=user, created_at=STAMP, updated_at=STAMP)
    for name, state in [('pending-proposal','pending'), ('accepted-proposal','accepted')]:
        add('milestone_proposal', id=ident(name), workspace_id=ws, milestone_id=ident('first-use') if state=='pending' else ident('launch'), proposed_status='achieved', proposed_actual_date='2026-10-01', evidence='Synthetic verifiable evidence.', evidence_refs=[{'type':'issue','id':issue}], proposed_by_type='agent', proposed_by_id=agent, source_issue_id=issue, state=state, decided_by_type='member' if state!='pending' else None, decided_by_id=user if state!='pending' else None, decided_at=STAMP if state!='pending' else None, created_at=STAMP)
    for name, enabled, status in [('installation',True,'active'), ('revoked-installation',False,'revoked')]:
        config={'bot_id':'synthetic-'+name, 'app_id':'synthetic-'+name, 'secret_encrypted':'c3ludGhldGljLW5vbi1jcmVkZW50aWFs', 'guest_access':{'enabled':enabled,'allowed_group_ids':['synthetic-group-a','synthetic-group-b'] if enabled else [],'allow_direct_messages':enabled,'sponsor_user_id':user,'version':ident(name+'-version'),'generation':ident(name+'-generation'),'updated_by':user,'updated_at':STAMP}}
        add('channel_installation', id=ident(name), workspace_id=ws, agent_id=agent if enabled else ident('revoked-agent'), channel_type='wecom', config=config, status=status, installer_user_id=user, installed_at=STAMP, created_at=STAMP, updated_at=STAMP)
    return 'BEGIN;\n' + '\n'.join(rows) + '\nCOMMIT;\n'


def snapshot(database, projection=None):
    # Batch introspection and fixture reads so Docker process startup does not
    # dominate the rehearsal. Explicit old columns permit additive upgrades.
    if projection:
        columns_by_table = {table: projection[table]['columns'] for table in TABLES}
    else:
        names = ','.join(map(literal, TABLES))
        columns_by_table = json.loads(query(database,
            "SELECT json_object_agg(table_name, columns) FROM ("
            "SELECT table_name,json_agg(column_name ORDER BY ordinal_position) AS columns "
            "FROM information_schema.columns WHERE table_schema='public' "
            "AND table_name IN (" + names + ") GROUP BY table_name) x"))
    fields = []
    for table in TABLES:
        cols = ','.join(map(quote, columns_by_table[table]))
        fields.extend([literal(table), "(SELECT coalesce(json_agg(r ORDER BY row_to_json(r)::text),'[]'::json) "
            "FROM (SELECT " + cols + " FROM " + quote(table) + ") r)"])
    data = json.loads(query(database, 'SELECT json_build_object(' + ','.join(fields) + ')'))
    result = {}
    for table in TABLES:
        rows = data[table]
        canonical = json.dumps(rows, sort_keys=True, separators=(',', ':')).encode()
        result[table] = {'columns': columns_by_table[table], 'count': len(rows),
            'sha256': hashlib.sha256(canonical).hexdigest(), 'rows': rows}
    return result


def ledger(database):
    return json.loads(query(database, "SELECT json_agg(r ORDER BY version) FROM (SELECT version, applied_at FROM schema_migrations) r"))


def save(name, value):
    (ARTIFACTS/name).write_text(json.dumps(value, indent=2, sort_keys=True)+'\n')


def migrate(server, database, phase):
    binary=ARTIFACTS/('migrate-'+phase)
    run(['go','build','-o',str(binary),'./cmd/migrate'], cwd=server)
    env=os.environ.copy()
    env['DATABASE_URL']='postgres://multica:multica@127.0.0.1:18562/'+database+'?sslmode=disable'
    started=time.monotonic()
    with (ARTIFACTS/(phase+'-migrate.log')).open('w') as log:
        run([str(binary),'up'],cwd=server,env=env,stdout=log,stderr=subprocess.STDOUT)
    return round(time.monotonic()-started,3)


def schema(database):
    data=run(['docker','exec',CONTAINER,'pg_dump','-U','multica','-d',database,'--schema-only','--no-owner','--no-privileges'],capture_output=True,text=True).stdout
    return '\n'.join(line for line in data.splitlines() if not line.startswith(('\\restrict','\\unrestrict')))


def create(database):
    exists=query('postgres', 'SELECT count(*) FROM pg_database WHERE datname='+literal(database))
    if exists != '0':
        raise RuntimeError('Refusing to overwrite existing database '+database)
    query('postgres','CREATE DATABASE '+quote(database))


def source_metadata(server, baseline):
    migration_digests={p.name:hashlib.sha256(p.read_bytes()).hexdigest()
        for p in sorted((server/'migrations').glob('*.up.sql'))}
    return {'baseline_commit':baseline['source_commit'],
        'target_release_commit':TARGET_RELEASE_COMMIT,
        'candidate_head_commit':run(['git','rev-parse','HEAD'],cwd=server,capture_output=True,text=True).stdout.strip(),
        'candidate_migration_tree_sha256':hashlib.sha256(json.dumps(migration_digests,sort_keys=True,separators=(',',':')).encode()).hexdigest(),
        'candidate_migrator_binary_sha256':hashlib.sha256((ARTIFACTS/'migrate-candidate').read_bytes()).hexdigest()}


def main():
    global ARTIFACTS
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase',choices=['baseline','upgrade','fresh','restore'])
    parser.add_argument('--server',type=Path,help='Source server directory; required except for restore')
    parser.add_argument('--artifacts',type=Path,default=ARTIFACTS,help='Use a new directory for each independent rehearsal')
    parser.add_argument('--database',default='multica_upgrade_base')
    args=parser.parse_args()
    ARTIFACTS=args.artifacts.resolve()
    if args.phase != 'restore' and args.server is None:
        parser.error('--server is required for this phase')
    if not re.fullmatch(r'multica_upgrade_[a-z0-9_]+',args.database):
        raise RuntimeError('Database must use the dedicated multica_upgrade_ prefix')
    inspected=json.loads(run(['docker','inspect',CONTAINER],capture_output=True,text=True).stdout)[0]
    if inspected['NetworkSettings']['Ports'].get('5432/tcp') != [{'HostIp':'127.0.0.1','HostPort':'18562'}]:
        raise RuntimeError('Dedicated container loopback port mismatch')
    ARTIFACTS.mkdir(parents=True,exist_ok=True)
    server=args.server.resolve() if args.server else None
    if args.phase=='baseline':
        if (ARTIFACTS/'baseline.json').exists():
            raise RuntimeError('Baseline already recorded; preserve evidence or select a separate artifact directory')
        create(args.database)
        duration=migrate(server,args.database,'base')
        if query(args.database,'SELECT count(*) FROM goal')!='0' or query(args.database,'SELECT count(*) FROM channel_installation')!='0':
            raise RuntimeError('Refusing to seed a populated database')
        sql=seed_sql()
        (ARTIFACTS/'fixture.sql').write_text(sql)
        query(args.database,sql)
        baseline={'database':args.database,'source_commit':run(['git','rev-parse','HEAD'],cwd=server,capture_output=True,text=True).stdout.strip(),'migration_seconds':duration,'rows':snapshot(args.database),'ledger':ledger(args.database),'migration_sha256':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((server/'migrations').glob('*.up.sql'))}}
        save('baseline.json',baseline)
        with (ARTIFACTS/'base-fixture.dump').open('wb') as output:
            run(['docker','exec',CONTAINER,'pg_dump','-U','multica','-d',args.database,'-Fc','--no-owner','--no-privileges'],stdout=output)
        (ARTIFACTS/'base-schema.sql').write_text(schema(args.database))
        print(json.dumps({'phase':'baseline','ledger_count':len(baseline['ledger']),'fixture_counts':{k:v['count'] for k,v in baseline['rows'].items()},'artifacts':str(ARTIFACTS)}))
    elif args.phase=='upgrade':
        baseline=json.loads((ARTIFACTS/'baseline.json').read_text())
        if args.database != baseline['database']:
            raise RuntimeError('Database does not match saved baseline')
        if snapshot(args.database,baseline['rows']) != baseline['rows']:
            raise RuntimeError('Fixture changed before candidate upgrade')
        before=ledger(args.database)
        if before != baseline['ledger']:
            raise RuntimeError('Migration ledger changed before candidate upgrade')
        changed={name:('missing' if not (server/'migrations'/name).exists() else 'changed') for name,digest in baseline['migration_sha256'].items() if not (server/'migrations'/name).exists() or hashlib.sha256((server/'migrations'/name).read_bytes()).hexdigest()!=digest}
        save('historical-migration-differences.json',changed)
        if changed:
            raise RuntimeError('Executed historical SQL changed; inspect historical-migration-differences.json before upgrading')
        duration=migrate(server,args.database,'candidate')
        first=snapshot(args.database,baseline['rows'])
        save('candidate-rows.json',first)
        if first != baseline['rows']:
            raise RuntimeError('Fixture rows changed during upgrade; inspect candidate-rows.json')
        after=ledger(args.database)
        if not all(row in after for row in before):
            raise RuntimeError('Existing migration ledger rows/timestamps changed')
        expected={p.name.removesuffix('.up.sql') for p in (server/'migrations').glob('*.up.sql')}
        if not expected.issubset({r['version'] for r in after}):
            raise RuntimeError('Candidate migration ledger incomplete')
        first_schema=schema(args.database)
        # Compare every fixture column after the first upgrade, including additions.
        complete_first=snapshot(args.database)
        again=migrate(server,args.database,'candidate-repeat')
        if ledger(args.database)!=after or snapshot(args.database)!=complete_first or schema(args.database)!=first_schema:
            raise RuntimeError('Second upgrade changed ledger, fixture rows or schema')
        save('candidate-ledger.json',after)
        changed={name:('missing' if not (server/'migrations'/name).exists() else 'changed') for name,digest in baseline['migration_sha256'].items() if not (server/'migrations'/name).exists() or hashlib.sha256((server/'migrations'/name).read_bytes()).hexdigest()!=digest}
        result={'source_metadata':source_metadata(server,baseline),'phase':'upgrade','database':args.database,'baseline_ledger_count':len(before),'candidate_ledger_count':len(after),'candidate_migration_file_count':len(expected),'added_ledger_versions':sorted({r['version'] for r in after}-{r['version'] for r in before}),'fixture_counts':{k:v['count'] for k,v in first.items()},'fixture_preserved':True,'old_ledger_timestamps_preserved':True,'second_upgrade_noop':True,'migration_seconds':duration,'repeat_seconds':again,'historical_migration_file_differences':changed}
        save('upgrade-result.json',result)
        print(json.dumps(result,sort_keys=True))
    elif args.phase=='restore':
        baseline=json.loads((ARTIFACTS/'baseline.json').read_text())
        create(args.database)
        with (ARTIFACTS/'base-fixture.dump').open('rb') as backup:
            run(['docker','exec','-i',CONTAINER,'pg_restore','-U','multica','-d',args.database,'--exit-on-error','--no-owner','--no-privileges'],stdin=backup)
        if snapshot(args.database,baseline['rows'])!=baseline['rows'] or ledger(args.database)!=baseline['ledger']:
            raise RuntimeError('Restored fixture or ledger differs from backup baseline')
        result={'phase':'restore','database':args.database,'fixture_preserved':True,'ledger_count':len(ledger(args.database)),'ledger_timestamps_preserved':True}
        save('backup-restore-result.json',result)
        print(json.dumps(result))
    else:
        if args.database=='multica_upgrade_base':
            raise RuntimeError('Fresh phase requires an unused --database name')
        create(args.database)
        duration=migrate(server,args.database,'fresh-'+args.database)
        actual=ledger(args.database)
        expected={p.name.removesuffix('.up.sql') for p in (server/'migrations').glob('*.up.sql')}
        if {r['version'] for r in actual}!=expected:
            raise RuntimeError('Fresh ledger differs from candidate files')
        query(args.database,seed_sql())
        first_rows=snapshot(args.database)
        first_schema=schema(args.database)
        migrate(server,args.database,'fresh-repeat-'+args.database)
        if ledger(args.database)!=actual or schema(args.database)!=first_schema or snapshot(args.database)!=first_rows:
            raise RuntimeError('Fresh installation repeat was not idempotent')
        result={'phase':'fresh','database':args.database,'candidate_migration_file_count':len(expected),'ledger_count':len(actual),'second_upgrade_noop':True,'migration_seconds':duration,'fixture_counts':{k:v['count'] for k,v in first_rows.items()}}
        save('fresh-result-'+args.database+'.json',result)
        print(json.dumps(result))


if __name__=='__main__':
    main()
