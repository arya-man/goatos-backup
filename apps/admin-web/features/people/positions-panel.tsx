'use client';

import { getAdminApi } from '@/lib/api/client';
import { useEffect, useState } from 'react';
import type { AdminApiComponents } from '@goatos/api-client';
import { optionalCopy, type AdminUiPageContract } from '@/lib/admin-ui-contract';

// Backend enriches positions with display fields from related tables
type BasePosition = AdminApiComponents['schemas']['Position'];
interface Position extends BasePosition {
  person_display_name?: string | null;
  hr_designation_grade?: string | null;
  position_title?: string | null;
  center_label?: string | null;
  tier?: string | null;
  week_off?: string | null;
  backup_group?: string | null;
}

type BackupConfig = AdminApiComponents['schemas']['BackupConfig'];
type Coverage = AdminApiComponents['schemas']['Coverage'];

interface PositionsPanelProps {
  pageContract?: AdminUiPageContract;
}

export function PositionsPanel({ pageContract }: PositionsPanelProps) {
  const [positions, setPositions] = useState<Position[]>([]);
  const [backupConfigs, setBackupConfigs] = useState<BackupConfig[]>([]);
  const [activeCoverages, setActiveCoverages] = useState<Coverage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Contract-driven labels (from pageContract, fallback to defaults)
  // Note: /people page contract copy/labels are being moved to backend; falling back to sensible defaults until complete
  const positionsTableLabel = pageContract?.tables?.find((t) => t.id === 'positions')?.title ?? 'Position & Coverage';
  const pageTitle = pageContract?.title ?? 'People / HRMS';
  const pageSubtitle = pageContract?.subtitle ?? 'Staff positions, coverage, timetable';

  const kpiPositionsFilled = optionalCopy(pageContract, 'kpi.positions_filled') ?? 'Positions filled';
  const kpiOffToday = optionalCopy(pageContract, 'kpi.off_today') ?? 'Off today (leave + week-off)';
  const kpiAutoCovered = optionalCopy(pageContract, 'kpi.auto_covered') ?? 'Auto-covered';
  const kpiEscalated = optionalCopy(pageContract, 'kpi.escalated') ?? 'Escalated to Park Head';

  useEffect(() => {
    const loadData = async () => {
      try {
        const api = getAdminApi();

        // Load positions
        const posResponse = await api.listStaffPositions();
        if (posResponse.data?.items) {
          setPositions(posResponse.data.items);
        }

        // Load backup configurations
        const backupResponse = await api.listBackupConfig();
        if (backupResponse.data?.items) {
          setBackupConfigs(backupResponse.data.items);
        }

        // Load active coverage
        const coverageResponse = await api.listCoverage();
        if (coverageResponse.data?.items) {
          setActiveCoverages(coverageResponse.data.items);
        }

        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load roster data');
        console.error('Error loading roster data:', err);
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
    return (
      <div style={{ padding: '16px', color: 'var(--danger)' }}>
        <svg className="ic" style={{ marginRight: '8px' }}>
          <use href="#i-warn" />
        </svg>
        Error: {error}
      </div>
    );
  }

  // Helper to determine if coverage is escalated based on backend coverage fields
  const isEscalatedCoverage = (coverage: Coverage): boolean => {
    return !!(coverage.escalation_state || coverage.source === 'escalation');
  };

  const positionsFilled = positions.filter(p => p.status === 'active').length;
  const autoCovered = activeCoverages.filter(c => c.source === 'leave' || c.source === 'week_off').length;
  const escalated = activeCoverages.filter(isEscalatedCoverage).length;

  return (
    <div className="subpanel" data-sub="positions">
      <div className="phead">
        <div>
          <div className="crumb">Team / <b>{positionsTableLabel}</b></div>
          <h1>{positionsTableLabel}</h1>
          <div className="sub">Each person holds a <b>fixed operational position</b> — never mutated by a leave. When someone is off (ad-hoc leave <b>or</b> their weekly OFF day), that position&apos;s <b>configured backup</b> covers only their due work for that window; ownership never changes. Functional managers never cross-cover.</div>
        </div>
        <div className="sp"></div>
        <button className="btn" title="How coverage works" disabled aria-label="How coverage works (backend contract pending)">
          <svg className="ic"><use href="#i-book"/></svg>How coverage works
        </button>
        <button className="btn p" title="Configure backup" disabled aria-label="Configure backup (backend contract pending)">
          <svg className="ic"><use href="#i-plus"/></svg>Configure backup
        </button>
      </div>

      <div className="grid g4" style={{ marginBottom: '16px' }}>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab"><svg className="ic"><use href="#i-people"/></svg>{kpiPositionsFilled}</div>
          <div className="val">{positionsFilled}</div>
          <div className="dl">{positions.length} total rows</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--amber)' }}></span>
          <div className="lab">{kpiOffToday}</div>
          <div className="val">{autoCovered + escalated}</div>
          <div className="dl"><span className="muted">all resolved below</span></div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab">{kpiAutoCovered}</div>
          <div className="val">{autoCovered}</div>
          <div className="dl up">backup routed</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--danger)' }}></span>
          <div className="lab">{kpiEscalated}</div>
          <div className="val">{escalated}</div>
          <div className="dl down">no backup configured</div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '16px' }}>
        <div className="hd">
          <svg className="ic" style={{ color: 'var(--brand)' }}><use href="#i-people"/></svg>
          <h3>Positions — three independent axes</h3>
          <div className="sp"></div>
          <span className="pill b">tier · position · backup</span>
        </div>
        <div className="bd" style={{ padding: 0 }}>
          <table>
            <thead>
              <tr>
                <th>Person</th>
                <th>Tier</th>
                <th>Position title</th>
                <th>Center</th>
                <th>Week OFF</th>
                <th>HR grade</th>
                <th>Backup group</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {positions.length === 0 ? (
                <tr>
                  <td colSpan={8} style={{ textAlign: 'center', padding: '16px', color: 'var(--muted)' }}>
                    No positions found
                  </td>
                </tr>
              ) : (
                positions.map((pos) => (
                  <tr key={pos.position_id} style={{ cursor: 'pointer' }}>
                    <td>{pos.person_display_name || '—'}</td>
                    <td><span className="tag t-info">{pos.tier || pos.position_tier}</span></td>
                    <td><b>{pos.position_title || pos.position_code}</b></td>
                    <td>{pos.center_label || '—'}</td>
                    <td>{pos.week_off || (pos.week_off_weekday ? pos.week_off_weekday : '—')}</td>
                    <td>{pos.hr_designation_grade || '—'}</td>
                    <td><span className="tag t-mut">{pos.backup_group || pos.backup_group_code || '—'}</span></td>
                    <td className="rowact">
                      <span className="ia" title="Edit position (backend endpoint pending)" aria-disabled="true">
                        <svg className="ic"><use href="#i-edit"/></svg>
                      </span>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
          <div className="note" style={{ margin: '12px 14px' }}>
            <b>Three axes stay independent:</b> HR designation grade (payroll context only), Position tier/title (who does what, from the timetable), and Backup group (coverage fallback). Tier never grants access; position code determines who can execute.
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
                  <th>Backup group</th>
                  <th>Backup position</th>
                  <th>Holder</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {backupConfigs.length === 0 ? (
                  <tr>
                    <td colSpan={5} style={{ textAlign: 'center', padding: '16px', color: 'var(--muted)' }}>
                      No backup configurations
                    </td>
                  </tr>
                ) : (
                  backupConfigs.map((config) => (
                    <tr key={`${config.center_id}-${config.backup_group_code}`}>
                      <td>{config.center_label || '—'}</td>
                      <td>{config.backup_group_code}</td>
                      <td><b>{config.backup_position_title || config.backup_position_code}</b></td>
                      <td>{config.configured_holder_name || '—'}</td>
                      <td>
                        <span className={`tag ${config.status === 'active' ? 't-ok' : 't-warn'}`}>
                          {config.status || 'configured'}
                        </span>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
            <div className="note" style={{ margin: '12px 14px' }}>
              Managers fall back to <b>one</b> center-wide Backup Manager; operators fall back to their <b>Backup AM tier</b> — never cross-cover. An empty slot escalates to the Park Head instead of silently picking someone.
            </div>
          </div>
        </div>

        <div className="card">
          <div className="hd">
            <svg className="ic" style={{ color: 'var(--amber)' }}><use href="#i-clock"/></svg>
            <h3>Active coverage — this period</h3>
            <div className="sp"></div>
            <span className="pill">ownership unchanged · window only</span>
          </div>
          <div className="bd feed">
            {activeCoverages.length === 0 ? (
              <div className="note" style={{ padding: '12px 14px', textAlign: 'center', color: 'var(--muted)' }}>
                No active coverage windows
              </div>
            ) : (
              activeCoverages.map((coverage) => {
                const isEscalated = isEscalatedCoverage(coverage);
                return (
                  <div key={coverage.position_id} className="fitem">
                    <span
                      className="fic"
                      style={{
                        background: isEscalated ? 'var(--dangerx)' : 'var(--okx)',
                        color: isEscalated ? 'var(--danger)' : 'var(--brand-d)'
                      }}
                    >
                      <svg className="ic">
                        <use href={isEscalated ? '#i-warn' : '#i-check'} />
                      </svg>
                    </span>
                    <div className="tx">
                      <b>{coverage.covered_position_title || coverage.covered_position_code}</b>
                      <div className="mt">
                        {coverage.covering_member_name || 'pending'} · {coverage.source}
                      </div>
                    </div>
                    <span className={`tag ${isEscalated ? 't-dng' : 't-ok'}`}>
                      {isEscalated ? 'escalated' : 'covered'}
                    </span>
                  </div>
                );
              })
            )}
            <div className="note" style={{ marginTop: '8px' }}>
              Each coverage grants the backup <b>execute permission for the window only</b> — it expires on its own. The absent person&apos;s ownership badge stays unchanged; only their due work reassigns.
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
