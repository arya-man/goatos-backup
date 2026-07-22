'use client';

import { getAdminApi } from '@/lib/api/client';
import { optionalCopy, type AdminUiPageContract } from '@/lib/admin-ui-contract';
import type { AdminApiComponents } from '@goatos/api-client';
import { CalendarDays, ShieldCheck, Stethoscope, TriangleAlert, UserRoundCheck, UsersRound } from 'lucide-react';
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
  const [operatorCap, setOperatorCap] = useState(DEFAULT_OPERATOR_CAP);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    async function loadData() {
      try {
        const api = getAdminApi();
        const [positionsResponse, capacityResponse] = await Promise.all([
          api.listStaffPositions(),
          api.getVaccinationCapacityConfig().catch(() => ({ data: { maxPerDay: DEFAULT_OPERATOR_CAP } })),
        ]);
        if (alive) {
          setPositions(positionsResponse.data?.items ?? []);
          setOperatorCap(capacityResponse.data.maxPerDay);
          setError(null);
        }
      } catch (err) {
        if (alive) setError(err instanceof Error ? err.message : 'Failed to load operator roster');
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
  const totalCapacity = operators.length * operatorCap;
  const title = pageContract?.tables?.find((t) => t.id === 'positions')?.title ?? 'Vaccination Operators';
  const subtitle =
    optionalCopy(pageContract, 'people.operator_roster.subtitle') ??
    'Operator availability drives vaccination planning. Directors monitor; they do not add field capacity unless explicitly assigned.';

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
          <div className="dl">animals per day</div>
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

      <div className="card">
        <div className="hd">
          <UserRoundCheck className="ic" style={{ color: 'var(--brand)' }} aria-hidden="true" />
          <h3>Vaccination drive roster</h3>
          <div className="sp"></div>
          <span className="pill b">operator cap · timetable · role</span>
        </div>
        <div className="bd" style={{ padding: 0 }}>
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
                  return (
                    <tr key={pos.position_id}>
                      <td><b>{personName(pos)}</b></td>
                      <td>{roleLabel(pos)}</td>
                      <td>{pos.center_label || '—'}</td>
                      <td>{director ? '—' : weekOff(pos)}</td>
                      <td>{director ? 'monitor only' : `${operatorCap} animals/day`}</td>
                      <td><span className="tag t-ok">{pos.status}</span></td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
          <div className="note" style={{ margin: '12px 14px' }}>
            Weekly off and leave remove an operator from that date&apos;s drive capacity. Chandrakant/director roles monitor across parks and do not count as field capacity unless explicitly assigned an execution role.
          </div>
        </div>
      </div>
    </div>
  );
}
