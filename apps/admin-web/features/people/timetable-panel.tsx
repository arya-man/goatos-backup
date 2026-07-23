'use client';

import { getAdminApi } from '@/lib/api/client';
import { useEffect, useMemo, useState } from 'react';
import { CalendarDays, Clock3, Pencil, ShieldCheck, Stethoscope, TriangleAlert, UserRoundCheck, UsersRound } from 'lucide-react';
import type { AdminApiComponents } from '@goatos/api-client';
import { type AdminUiPageContract } from '@/lib/admin-ui-contract';

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

interface TimetablePanelProps {
  pageContract?: AdminUiPageContract;
  onEditCaps?: () => void;
}

const DEFAULT_OPERATOR_CAP = 200;
const WEEKDAYS = [
  { key: 'monday', label: 'Mon' },
  { key: 'tuesday', label: 'Tue' },
  { key: 'wednesday', label: 'Wed' },
  { key: 'thursday', label: 'Thu' },
  { key: 'friday', label: 'Fri' },
  { key: 'saturday', label: 'Sat' },
  { key: 'sunday', label: 'Sun' },
] as const;

function dutyMatchesVaccinationExecute(pos: Position): boolean {
  return (pos.duties ?? []).some((duty) => duty.module_code === 'pc.vaccination' && duty.duty_type === 'execute');
}

function looksLikeVaccinationOperator(pos: Position): boolean {
  const haystack = `${pos.position_code ?? ''} ${pos.position_title ?? ''} ${pos.tier ?? ''}`.toLowerCase();
  return haystack.includes('vaccination') || haystack.includes('preventive_care') || haystack.includes('operator');
}

function isCptSeat(pos: Position): boolean {
  const haystack = `${pos.center_label ?? ''} ${pos.position_code ?? ''}`.toLowerCase();
  return haystack.includes('cpt') || haystack.includes('channapatna') || haystack.includes('vaccination_operator_');
}

function isFieldOperator(pos: Position): boolean {
  return pos.status === 'active' && !pos.is_backup_slot && pos.position_tier !== 'director' && isCptSeat(pos) && (dutyMatchesVaccinationExecute(pos) || looksLikeVaccinationOperator(pos));
}

function personName(pos: Position): string {
  return pos.person_display_name || pos.position_title || pos.position_code || 'Unassigned';
}

function weekOffKey(pos: Position): string | null {
  return pos.week_off_weekday ?? (pos.week_off ? pos.week_off.toLowerCase() : null);
}

function weekOffLabel(pos: Position): string {
  const key = weekOffKey(pos);
  const day = WEEKDAYS.find((weekday) => weekday.key === key);
  return day?.label ?? pos.week_off ?? '—';
}

function operatorDailyCap(pos: Position, fallback: number): number {
  return pos.vaccination_daily_animal_cap ?? fallback;
}

export function TimetablePanel({ pageContract, onEditCaps }: TimetablePanelProps) {
  const [positions, setPositions] = useState<Position[]>([]);
  const [operatorCap, setOperatorCap] = useState(DEFAULT_OPERATOR_CAP);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    const loadData = async () => {
      try {
        const api = getAdminApi();
        const [positionsResponse, capacityResponse] = await Promise.all([
          api.listStaffPositions({ status: 'active', limit: 500 }),
          api.getVaccinationCapacityConfig().catch(() => ({ data: { maxPerDay: DEFAULT_OPERATOR_CAP } })),
        ]);

        if (alive) {
          setPositions(positionsResponse.data?.items ?? []);
          setOperatorCap(capacityResponse.data.maxPerDay ?? DEFAULT_OPERATOR_CAP);
          setError(null);
        }
      } catch (err) {
        if (alive) setError(err instanceof Error ? err.message : 'Failed to load timetable');
      } finally {
        if (alive) setLoading(false);
      }
    };

    loadData();
    return () => {
      alive = false;
    };
  }, []);

  const operators = useMemo(() => positions.filter(isFieldOperator).sort((a, b) => personName(a).localeCompare(personName(b))), [positions]);
  const activeDays = useMemo(
    () =>
      WEEKDAYS.map((day) =>
        operators
          .filter((operator) => weekOffKey(operator) !== day.key)
          .reduce((sum, operator) => sum + operatorDailyCap(operator, operatorCap), 0),
      ),
    [operatorCap, operators],
  );
  const peakDailyCapacity = operators.reduce((sum, operator) => sum + operatorDailyCap(operator, operatorCap), 0);
  const lowestDailyCapacity = activeDays.length > 0 ? Math.min(...activeDays) : 0;
  const customCapCount = operators.filter((operator) => operator.vaccination_daily_animal_cap != null).length;
  const title = pageContract?.tables?.find((table) => table.id === 'timetable')?.title ?? 'Vaccination operator timetable';

  if (loading) {
    return (
      <div className="subpanel on" data-sub="timetable">
        <div className="note" style={{ padding: 16 }}>Loading vaccination operator timetable...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="subpanel on" data-sub="timetable">
        <div className="note" style={{ padding: 16, color: 'var(--danger)' }}>
          <TriangleAlert className="ic" style={{ marginRight: '8px' }} aria-hidden="true" />
          {error}
        </div>
      </div>
    );
  }

  return (
    <div className="subpanel on" data-sub="timetable">
      <div className="phead">
        <div>
          <div className="crumb">Team / <b>Vaccination availability</b></div>
          <h1>{title}</h1>
          <div className="sub">
            Backend roster and capacity data for CPT vaccination execution. Week-off days remove that operator from drive capacity.
          </div>
        </div>
        {onEditCaps ? (
          <button className="btn" onClick={onEditCaps} type="button">
            <Pencil className="ic" aria-hidden="true" />
            Edit drive caps
          </button>
        ) : null}
      </div>

      <div className="grid g4 people-availability-kpis" style={{ marginBottom: 16 }}>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab"><UsersRound className="ic" aria-hidden="true" />CPT operators</div>
          <div className="val">{operators.length}</div>
          <div className="dl">active vaccination seats</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--amber)' }}></span>
          <div className="lab"><Stethoscope className="ic" aria-hidden="true" />Cap / operator</div>
          <div className="val">{operatorCap}</div>
          <div className="dl">default · {customCapCount} custom</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab"><CalendarDays className="ic" aria-hidden="true" />Full-cap day</div>
          <div className="val">{peakDailyCapacity}</div>
          <div className="dl">when all are available</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--teal)' }}></span>
          <div className="lab"><ShieldCheck className="ic" aria-hidden="true" />Lowest day</div>
          <div className="val">{lowestDailyCapacity}</div>
          <div className="dl">after scheduled week-off</div>
        </div>
      </div>

      <div className="card people-availability-card">
        <div className="hd">
          <Clock3 className="ic" style={{ color: 'var(--brand)' }} aria-hidden="true" />
          <h3>Weekly availability</h3>
          <div className="sp"></div>
          <span className="pill b">active · week-off · HRMS cap</span>
        </div>
        <div className="bd people-availability-body">
          {operators.length === 0 ? (
            <div className="note people-availability-empty">
              No active CPT vaccination operator seats returned by the roster API.
            </div>
          ) : (
            <div className="people-availability-list">
              {operators.map((operator) => (
                <div className="people-operator-row" key={operator.position_id}>
                  <div className="people-operator-main">
                    <UserRoundCheck className="ic" aria-hidden="true" />
                    <div className="people-operator-text">
                      <b>{personName(operator)}</b>
                      <span>{operator.position_title || operator.position_code}</span>
                    </div>
                  </div>
                  <div className="people-operator-meta">
                    <span className="tag t-ok">{operator.status}</span>
                    <span className="tag t-info">{weekOffLabel(operator)} off</span>
                    <span className="tag t-teal">{operatorDailyCap(operator, operatorCap)} animals/day</span>
                    {onEditCaps ? (
                      <button className="btn" onClick={onEditCaps} type="button">
                        Edit cap
                      </button>
                    ) : null}
                  </div>
                  <div className="people-week-grid" aria-label={`${personName(operator)} weekly availability`}>
                    {WEEKDAYS.map((day) => {
                      const off = weekOffKey(operator) === day.key;
                      return (
                        <div className={`people-week-cell ${off ? 'off' : 'on'}`} key={day.key}>
                          <span>{day.label}</span>
                          <b>{off ? 'Off' : 'Avail'}</b>
                        </div>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
