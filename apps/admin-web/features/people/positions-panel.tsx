'use client';

import { getAdminApi } from '@/lib/api/client';
import { optionalCopy, type AdminUiPageContract } from '@/lib/admin-ui-contract';
import type { AdminApiComponents } from '@goatos/api-client';
import { CalendarDays, Save, ShieldCheck, Stethoscope, TriangleAlert, UserRoundCheck, UsersRound, X, Users } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';

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
type PositionProfile = AdminApiComponents['schemas']['PositionProfile'];

interface PositionsPanelProps {
  pageContract?: AdminUiPageContract;
}

const DEFAULT_OPERATOR_CAP = 200;

function isVaccinationOperator(pos: Position): boolean {
  const haystack = `${pos.position_code ?? ''} ${pos.position_title ?? ''} ${pos.tier ?? ''}`.toLowerCase();
  return haystack.includes('vaccination') || haystack.includes('operator') || haystack.includes('preventive_care');
}

function isDirector(pos: Position): boolean {
  const haystack = `${pos.position_code ?? ''} ${pos.position_title ?? ''} ${pos.tier ?? ''}`.toLowerCase();
  return haystack.includes('director');
}

function personName(pos: Position): string {
  return pos.person_display_name || pos.position_title || pos.position_code || 'Unassigned';
}

function roleLabel(pos: Position): string {
  if (isDirector(pos)) return 'Preventive Care Director';
  if (isVaccinationOperator(pos)) return 'Vaccination Operator';
  return pos.position_title || pos.position_code || 'Role';
}

function weekOff(pos: Position): string {
  return pos.week_off || pos.week_off_weekday || '—';
}

export function PositionsPanel({ pageContract }: PositionsPanelProps) {
  const [positions, setPositions] = useState<Position[]>([]);
  const [backupConfigs, setBackupConfigs] = useState<BackupConfig[]>([]);
  const [activeCoverages, setActiveCoverages] = useState<Coverage[]>([]);
  const [operatorCap, setOperatorCap] = useState(DEFAULT_OPERATOR_CAP);
  const [draftCaps, setDraftCaps] = useState<Record<string, string>>({});
  const [savingCap, setSavingCap] = useState<string | null>(null);
  const [capError, setCapError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [profile, setProfile] = useState<PositionProfile | null>(null);
  const [profileLoading, setProfileLoading] = useState(false);
  const [profileError, setProfileError] = useState<string | null>(null);

  const openProfile = async (positionId: string) => {
    setProfile(null);
    setProfileError(null);
    setProfileLoading(true);
    try {
      const res = await getAdminApi().getStaffPositionProfile(positionId);
      setProfile(res.data.profile ?? null);
    } catch (err) {
      setProfileError(err instanceof Error ? err.message : 'Failed to load position profile');
    } finally {
      setProfileLoading(false);
    }
  };

  const closeProfile = () => {
    setProfile(null);
    setProfileError(null);
    setProfileLoading(false);
  };

  // Client-local overlay UX: close the profile drawer on Escape (X and outside/backdrop
  // click are wired on the drawer itself). No navigation/URL — pure local state.
  const profileOpen = profileLoading || profile != null || profileError != null;
  useEffect(() => {
    if (!profileOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeProfile();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [profileOpen]);

  useEffect(() => {
    let alive = true;
    async function loadData() {
      try {
        const api = getAdminApi();
        const [positionsResponse, capacityResponse, backupResponse, coverageResponse] = await Promise.all([
          api.listStaffPositions(),
          api.getVaccinationCapacityConfig().catch(() => ({ data: { maxPerDay: DEFAULT_OPERATOR_CAP } })),
          api.listBackupConfig().catch(() => ({ data: { items: [] } })),
          api.listCoverage().catch(() => ({ data: { items: [] } })),
        ]);
        if (alive) {
          setPositions(positionsResponse.data?.items ?? []);
          setOperatorCap(capacityResponse.data.maxPerDay);
          setBackupConfigs(backupResponse.data?.items ?? []);
          setActiveCoverages(coverageResponse.data?.items ?? []);
          setError(null);
        }
      } catch (err) {
        if (alive) setError(err instanceof Error ? err.message : 'Failed to load roster data');
      } finally {
        if (alive) setLoading(false);
      }
    }
    loadData();
    return () => {
      alive = false;
    };
  }, []);

  const roster = useMemo(() => {
    return positions.filter((pos) => pos.status === 'active' && (isVaccinationOperator(pos) || isDirector(pos)));
  }, [positions]);

  const operators = roster.filter((pos) => !isDirector(pos));
  const directors = roster.filter(isDirector);
  const capForPosition = (pos: Position) => pos.vaccination_daily_animal_cap ?? operatorCap;
  const totalCapacity = operators.reduce((sum, pos) => sum + capForPosition(pos), 0);
  const customCapCount = operators.filter((pos) => pos.vaccination_daily_animal_cap != null).length;
  const title = pageContract?.tables?.find((t) => t.id === 'positions')?.title ?? 'Vaccination Operators';
  const subtitle =
    optionalCopy(pageContract, 'people.operator_roster.subtitle') ??
    'Operator availability drives vaccination planning. Directors monitor; they do not add field capacity unless explicitly assigned.';

  async function saveOperatorCap(pos: Position) {
    const raw = (draftCaps[pos.position_id] ?? String(capForPosition(pos))).trim();
    const nextCap = Number(raw);
    if (!Number.isInteger(nextCap) || nextCap < 1 || nextCap > 100000) {
      setCapError('Drive cap must be a whole number between 1 and 100000 animals/day.');
      return;
    }
    setSavingCap(pos.position_id);
    setCapError(null);
    try {
      const api = getAdminApi();
      const response = await api.updateStaffPosition(pos.position_id, {
        row_version: pos.row_version,
        vaccination_daily_animal_cap: nextCap,
      });
      const updated = response.data.position as Position;
      setPositions((current) => current.map((item) => (item.position_id === updated.position_id ? { ...item, ...updated } : item)));
      setDraftCaps((current) => {
        const next = { ...current };
        delete next[pos.position_id];
        return next;
      });
    } catch (err) {
      setCapError(err instanceof Error ? err.message : 'Failed to save operator cap');
    } finally {
      setSavingCap(null);
    }
  }

  async function clearOperatorCap(pos: Position) {
    setSavingCap(pos.position_id);
    setCapError(null);
    try {
      const api = getAdminApi();
      const response = await api.updateStaffPosition(pos.position_id, {
        row_version: pos.row_version,
        vaccination_daily_animal_cap: null,
      });
      const updated = response.data.position as Position;
      setPositions((current) => current.map((item) => (item.position_id === updated.position_id ? { ...item, ...updated } : item)));
      setDraftCaps((current) => {
        const next = { ...current };
        delete next[pos.position_id];
        return next;
      });
    } catch (err) {
      setCapError(err instanceof Error ? err.message : 'Failed to clear operator cap');
    } finally {
      setSavingCap(null);
    }
  }

  if (loading) {
    return (
      <div className="subpanel on" data-sub="positions">
        <div className="note" style={{ padding: 16 }}>Loading operator roster...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="subpanel on" data-sub="positions">
        <div className="note" style={{ color: 'var(--danger)', padding: 16 }}>
          <TriangleAlert className="ic" style={{ marginRight: 8 }} aria-hidden="true" />
          {error}
        </div>
      </div>
    );
  }

  return (
    <div className="subpanel on" data-sub="positions">
      <div className="phead">
        <div>
          <div className="crumb">Team / <b>{title}</b></div>
          <h1>{title}</h1>
          <div className="sub">{subtitle}</div>
        </div>
      </div>

      <div className="grid g4" style={{ marginBottom: 16 }}>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab"><UsersRound className="ic" aria-hidden="true" />Operators</div>
          <div className="val">{operators.length}</div>
          <div className="dl">field capacity seats</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--amber)' }}></span>
          <div className="lab"><Stethoscope className="ic" aria-hidden="true" />Cap / operator</div>
          <div className="val">{operatorCap}</div>
          <div className="dl">default · {customCapCount} custom</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab"><CalendarDays className="ic" aria-hidden="true" />Daily capacity</div>
          <div className="val">{totalCapacity}</div>
          <div className="dl">before leave/day-off</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--teal)' }}></span>
          <div className="lab"><ShieldCheck className="ic" aria-hidden="true" />Directors</div>
          <div className="val">{directors.length}</div>
          <div className="dl">monitoring only</div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: '16px' }}>
        <div className="hd">
          <Users className="ic" style={{ color: 'var(--brand)' }} aria-hidden="true" />
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
                  <tr
                    key={pos.position_id}
                    style={{ cursor: 'pointer' }}
                    onClick={() => void openProfile(pos.position_id)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        void openProfile(pos.position_id);
                      }
                    }}
                    tabIndex={0}
                    role="button"
                    aria-label={`Open profile for ${pos.person_display_name || pos.position_code}`}
                  >
                    <td>{pos.person_display_name || '—'}</td>
                    <td><span className="tag t-info">{pos.tier || pos.position_tier}</span></td>
                    <td><b>{pos.position_title || pos.position_code}</b></td>
                    <td>{pos.center_label || '—'}</td>
                    <td>{pos.week_off || (pos.week_off_weekday ? pos.week_off_weekday : '—')}</td>
                    <td>{pos.hr_designation_grade || '—'}</td>
                    <td><span className="tag t-mut">{pos.backup_group || pos.backup_group_code || '—'}</span></td>
                    <td className="rowact">
                      <span className="ia" title="Open position profile">
                        <svg className="ic" aria-hidden="true"><use href="#i-edit"/></svg>
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
            <Users className="ic" style={{ color: 'var(--brand)' }} aria-hidden="true" />
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
            <CalendarDays className="ic" style={{ color: 'var(--amber)' }} aria-hidden="true" />
            <h3>Active coverage — this period</h3>
            <div className="sp"></div>
            <span className="pill">ownership unchanged · window only</span>
          </div>
          <div className="bd feed">
            {activeCoverages.length === 0 ? (
              <div style={{ textAlign: 'center', padding: '16px', color: 'var(--muted)' }}>
                No active coverage
              </div>
            ) : (
              activeCoverages.map((coverage) => (
                <div key={`${coverage.position_id}-${coverage.start_date}`} style={{ padding: '12px 14px', borderBottom: '1px solid var(--border)', display: 'flex', gap: 12, alignItems: 'flex-start' }}>
                  <div style={{ flex: 1 }}>
                    <div style={{ fontWeight: 'bold' }}>{coverage.covering_member_name || coverage.covered_position_title || coverage.covered_position_code || '—'}</div>
                    <div style={{ fontSize: '0.875rem', color: 'var(--muted)' }}>{coverage.source}</div>
                  </div>
                  <div style={{ fontSize: '0.875rem', color: 'var(--muted)', textAlign: 'right' }}>
                    {coverage.start_date && coverage.end_date ? `${coverage.start_date} to ${coverage.end_date}` : '—'}
                  </div>
                </div>
              ))
            )}
            <div className="note" style={{ margin: '12px 14px' }}>
              Each coverage grants the backup <b>execute permission for the window only</b> — it expires on its own. The absent person&apos;s ownership badge stays unchanged; only their due work reassigns.
            </div>
          </div>
        </div>
      </div>

      <div className="card">
        <div className="hd">
          <UserRoundCheck className="ic" style={{ color: 'var(--brand)' }} aria-hidden="true" />
          <h3>Vaccination drive roster</h3>
          <div className="sp"></div>
          <span className="pill b">operator cap · timetable · role</span>
        </div>
        <div className="bd" style={{ padding: 0 }}>
          {capError ? (
            <div className="note" style={{ color: 'var(--danger)', margin: '12px 14px' }}>
              <TriangleAlert className="ic" style={{ marginRight: 8 }} aria-hidden="true" />
              {capError}
            </div>
          ) : null}
          <table>
            <thead>
              <tr>
                <th>Person</th>
                <th>Role</th>
                <th>Park / center</th>
                <th>Week off</th>
                <th>Drive cap</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {roster.length === 0 ? (
                <tr>
                  <td colSpan={6} style={{ color: 'var(--muted)', padding: 16, textAlign: 'center' }}>
                    No vaccination roster configured
                  </td>
                </tr>
              ) : (
                roster.map((pos) => {
                  const director = isDirector(pos);
                  const cap = capForPosition(pos);
                  const draft = draftCaps[pos.position_id] ?? String(cap);
                  const changed = Number(draft) !== cap;
                  return (
                    <tr key={pos.position_id}>
                      <td><b>{personName(pos)}</b></td>
                      <td>{roleLabel(pos)}</td>
                      <td>{pos.center_label || '—'}</td>
                      <td>{director ? '—' : weekOff(pos)}</td>
                      <td>
                        {director ? (
                          'monitor only'
                        ) : (
                          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                            <input
                              aria-label={`${personName(pos)} drive cap`}
                              inputMode="numeric"
                              min={1}
                              max={100000}
                              style={{ width: 96 }}
                              type="number"
                              value={draft}
                              onChange={(event) => setDraftCaps((current) => ({ ...current, [pos.position_id]: event.target.value }))}
                            />
                            <span className="tag t-teal">animals/day</span>
                            <button
                              className="btn"
                              disabled={!changed || savingCap === pos.position_id}
                              onClick={() => void saveOperatorCap(pos)}
                              type="button"
                            >
                              <Save className="ic" aria-hidden="true" />
                              {savingCap === pos.position_id ? 'Saving' : 'Save'}
                            </button>
                            {pos.vaccination_daily_animal_cap != null ? (
                              <button
                                className="btn"
                                disabled={savingCap === pos.position_id}
                                onClick={() => void clearOperatorCap(pos)}
                                type="button"
                              >
                                Use default
                              </button>
                            ) : null}
                          </div>
                        )}
                      </td>
                      <td><span className="tag t-ok">{pos.status}</span></td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
          <div className="note" style={{ margin: '12px 14px' }}>
            HRMS drive cap is the scheduler source of truth for vaccination animal/day capacity. Weekly off and leave remove an operator from that date&apos;s drive capacity.
          </div>
        </div>
      </div>

      {profileLoading ? (
        <div style={{ padding: '16px', textAlign: 'center', color: 'var(--muted)' }}>
          Loading position profile...
        </div>
      ) : null}
      {profileError ? (
        <div style={{ padding: '16px', color: 'var(--danger)' }}>
          <TriangleAlert className="ic" style={{ marginRight: 8 }} aria-hidden="true" />
          {profileError}
        </div>
      ) : null}
      {profile ? (
        <>
        <div
          onClick={closeProfile}
          aria-hidden="true"
          style={{ position: 'fixed', inset: 0, zIndex: 999, background: 'transparent' }}
        />
        <div role="dialog" aria-label="Position profile" style={{ position: 'fixed', top: 0, right: 0, bottom: 0, width: '400px', background: 'var(--bg)', borderLeft: '1px solid var(--border)', zIndex: 1000, overflow: 'auto', display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '16px', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <h2 style={{ margin: 0 }}>Position Profile</h2>
            <button onClick={closeProfile} className="btn s" title="Close" aria-label="Close drawer">
              <X className="ic" aria-hidden="true" />
            </button>
          </div>
          <div style={{ padding: '16px', flex: 1, overflow: 'auto' }}>
            <div style={{ marginBottom: '16px' }}>
              <div style={{ color: 'var(--muted)', fontSize: '0.875rem' }}>Person</div>
              <div style={{ fontWeight: 'bold' }}>{profile.position.person_display_name || profile.position.position_code || '—'}</div>
            </div>
            <div style={{ marginBottom: '16px' }}>
              <div style={{ color: 'var(--muted)', fontSize: '0.875rem' }}>Position</div>
              <div style={{ fontWeight: 'bold' }}>{profile.position.position_title || profile.position.position_code || '—'}</div>
            </div>
            <div style={{ marginBottom: '16px' }}>
              <div style={{ color: 'var(--muted)', fontSize: '0.875rem' }}>Tier</div>
              <div>{profile.position.tier || profile.position.position_tier || '—'}</div>
            </div>
            <div style={{ marginBottom: '16px' }}>
              <div style={{ color: 'var(--muted)', fontSize: '0.875rem' }}>Center</div>
              <div>{profile.position.center_label || '—'}</div>
            </div>
            <div style={{ marginBottom: '16px' }}>
              <div style={{ color: 'var(--muted)', fontSize: '0.875rem' }}>Week OFF</div>
              <div>{profile.position.week_off || profile.position.week_off_weekday || '—'}</div>
            </div>
            <div style={{ marginBottom: '16px' }}>
              <div style={{ color: 'var(--muted)', fontSize: '0.875rem' }}>Backup Group</div>
              <div>{profile.position.backup_group_code || '—'}</div>
            </div>
            <div style={{ marginBottom: '16px' }}>
              <div style={{ color: 'var(--muted)', fontSize: '0.875rem' }}>Status</div>
              <div><span className="tag t-ok">{profile.position.status || 'active'}</span></div>
            </div>
          </div>
        </div>
        </>
      ) : null}
    </div>
  );
}
