'use client';

import { getAdminApi } from '@/lib/api/client';
import { useEffect, useState } from 'react';

interface Position {
  position_id: string;
  position_code: string;
  position_title: string;
  person_name: string;
  hr_grade: string;
  center: string;
  week_off_day: string;
  department: string;
  backup_group: string;
}

interface BackupConfiguration {
  center: string;
  group: string;
  covers: string;
  backup_slot: string;
  is_configured: boolean;
}

interface ActiveCoverage {
  position_name: string;
  center: string;
  reason: string;
  coverage_type: string;
  backup_name: string;
  status: 'auto-covered' | 'escalated';
}

export function PositionsPanel() {
  const [positions, setPositions] = useState<Position[]>([]);
  const [backupConfigs, setBackupConfigs] = useState<BackupConfiguration[]>([]);
  const [activeCoverages, setActiveCoverages] = useState<ActiveCoverage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadData = async () => {
      try {
        const api = getAdminApi();
        const response = await api.getAdminRosterPositions({});

        if (response.data) {
          // Transform API response into UI format
          setPositions(response.data as Position[]);
        }

        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load positions');
        console.error('Error loading positions:', err);
      } finally {
        setLoading(false);
      }
    };

    loadData();
  }, []);

  if (loading) {
    return <div className="p-4 text-center">Loading positions...</div>;
  }

  if (error) {
    return <div className="p-4 text-red-600">Error: {error}</div>;
  }

  const positionsFilled = positions.length;
  const offToday = activeCoverages.filter(c => c.status === 'auto-covered' || c.status === 'escalated').length;
  const autoCovered = activeCoverages.filter(c => c.status === 'auto-covered').length;
  const escalated = activeCoverages.filter(c => c.status === 'escalated').length;

  return (
    <div className="subpanel" data-sub="positions">
      <div className="phead">
        <div>
          <div className="crumb">Team / <b>Position &amp; Coverage</b></div>
          <h1>Position &amp; Coverage</h1>
          <div className="sub">Each person holds a <b>fixed operational position</b> — never mutated by a leave. When someone is off (ad-hoc leave <b>or</b> their weekly OFF day), that position's <b>configured backup</b> covers only their due work for that window; ownership never changes. Functional managers never cross-cover.</div>
        </div>
        <div className="sp"></div>
        <button className="btn" title="How coverage works">
          <svg className="ic"><use href="#i-book"/></svg>How coverage works
        </button>
        <button className="btn p" title="Configure backup">
          <svg className="ic"><use href="#i-plus"/></svg>Configure backup
        </button>
      </div>

      <div className="grid g4" style={{ marginBottom: '16px' }}>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab"><svg className="ic"><use href="#i-people"/></svg>Positions filled</div>
          <div className="val">{positionsFilled}<small>/25</small></div>
          <div className="dl down">{25 - positionsFilled} slots open</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--amber)' }}></span>
          <div className="lab">Off today (leave + week-off)</div>
          <div className="val">{offToday}</div>
          <div className="dl"><span className="muted">all resolved below</span></div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab">Auto-covered</div>
          <div className="val">{autoCovered}</div>
          <div className="dl up">backup routed</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--danger)' }}></span>
          <div className="lab">Escalated to Park Head</div>
          <div className="val">{escalated}</div>
          <div className="dl down">no backup configured</div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '16px' }}>
        <div className="hd">
          <svg className="ic" style={{ color: 'var(--brand)' }}><use href="#i-people"/></svg>
          <h3>Positions — three independent axes</h3>
          <div className="sp"></div>
          <span className="pill b">grade · position · department are separate</span>
        </div>
        <div className="bd" style={{ padding: 0 }}>
          <table>
            <thead>
              <tr>
                <th>Person</th>
                <th>HR grade <span className="small muted">(a)</span></th>
                <th>Operational position <span className="small muted">(b)</span></th>
                <th>Center</th>
                <th>Week OFF</th>
                <th>Department owned <span className="small muted">(c)</span></th>
                <th>Backup group</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {positions.map((pos) => (
                <tr key={pos.position_id} onClick={() => {}} style={{ cursor: 'pointer' }}>
                  <td>{pos.person_name}</td>
                  <td><span className="tag t-info">{pos.hr_grade}</span></td>
                  <td><b>{pos.position_title}</b></td>
                  <td>{pos.center}</td>
                  <td>{pos.week_off_day}</td>
                  <td>{pos.department}</td>
                  <td><span className="tag t-mut">{pos.backup_group}</span></td>
                  <td className="rowact">
                    <span className="ia" onClick={(e) => e.stopPropagation()}>
                      <svg className="ic"><use href="#i-edit"/></svg>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="note" style={{ margin: '12px 14px' }}>
            <b>Three axes stay independent:</b> HR grade (CXO/Director/Manager/Assistant — payroll context only), Operational position (who does what, from the timetable), and Department ownership (which product modules a person's department owns → drives the sidebar). Grade never grants access; position + department do.
          </div>
        </div>
      </div>

      <div className="grid g2">
        <div className="card">
          <div className="hd">
            <svg className="ic" style={{ color: 'var(--brand)' }}><use href="#i-people"/></svg>
            <h3>Configured backup — per center × group</h3>
            <span className="small muted" style={{ marginLeft: 'auto' }}>two-tier · fixed slots</span>
          </div>
          <div className="bd" style={{ padding: 0 }}>
            <table>
              <thead>
                <tr>
                  <th>Center</th>
                  <th>Group</th>
                  <th>Covers</th>
                  <th>Backup slot</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {backupConfigs.map((config, idx) => (
                  <tr key={idx}>
                    <td>{config.center}</td>
                    <td>{config.group}</td>
                    <td>{config.covers}</td>
                    <td>{config.backup_slot}</td>
                    <td>
                      <span className={`tag ${config.is_configured ? 't-ok' : 't-warn'}`}>
                        {config.is_configured ? 'configured' : 'not configured'}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="note" style={{ margin: '12px 14px' }}>
              Managers fall back to <b>one</b> Backup Manager per center; operators fall back to their own <b>Backup AM1/AM2</b> — never the Backup Manager, never another functional manager. An empty slot escalates to the Park Head instead of silently picking someone.
            </div>
          </div>
        </div>

        <div className="card">
          <div className="hd">
            <svg className="ic" style={{ color: 'var(--amber)' }}><use href="#i-clock"/></svg>
            <h3>Active coverage — today</h3>
            <div className="sp"></div>
            <span className="pill">ownership unchanged · window only</span>
          </div>
          <div className="bd feed">
            {activeCoverages.length === 0 ? (
              <div className="note" style={{ padding: '12px 14px', textAlign: 'center', color: 'var(--muted)' }}>
                No active coverage windows today
              </div>
            ) : (
              activeCoverages.map((coverage, idx) => (
                <div key={idx} className="fitem">
                  <span
                    className="fic"
                    style={{
                      background: coverage.status === 'auto-covered' ? 'var(--okx)' : 'var(--dangerx)',
                      color: coverage.status === 'auto-covered' ? 'var(--brand-d)' : 'var(--danger)'
                    }}
                  >
                    <svg className="ic">
                      <use href={coverage.status === 'auto-covered' ? '#i-check' : '#i-warn'}/>
                    </svg>
                  </span>
                  <div className="tx">
                    <b>{coverage.position_name} · {coverage.center} — {coverage.reason}</b>
                    <div className="mt">{coverage.backup_name} covering</div>
                  </div>
                  <span className={`tag ${coverage.status === 'auto-covered' ? 't-ok' : 't-dng'}`}>
                    {coverage.status === 'auto-covered' ? 'auto-covered' : 'escalated'}
                  </span>
                </div>
              ))
            )}
            <div className="note" style={{ marginTop: '8px' }}>
              Each coverage grants the backup <b>execute permission for the window only</b> — it expires on its own. The absent person's ownership badge is untouched; only their due tasks for those dates reassign.
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
