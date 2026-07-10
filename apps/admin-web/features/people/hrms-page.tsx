'use client';

import { PositionsPanel } from './positions-panel';
import { TimetablePanel } from './timetable-panel';
import { type RouteSearchParams } from '@/lib/search-params';
import { type AdminUiPageContract } from '@/lib/admin-ui-contract';

interface HRMSPageProps {
  searchParams: RouteSearchParams;
  tab: string;
  pageContract?: AdminUiPageContract;
}

export function HRMSPage({ tab, pageContract }: HRMSPageProps) {
  // Render tabs from pageContract.tables, with fallback to defaults
  const positionsLabel = pageContract?.tables?.find((t) => t.id === 'positions')?.label ?? 'Position & Coverage';
  const timetableLabel = pageContract?.tables?.find((t) => t.id === 'timetable')?.label ?? 'Timetable';

  return (
    <section className="screen" data-screen="people">
      <div className="subtabs">
        <button
          data-screen="people"
          data-sub="positions"
          className={tab === 'positions' ? 'on' : ''}
          onClick={() => {
            const url = new URL(window.location.href);
            url.searchParams.set('tab', 'positions');
            window.history.pushState({}, '', url);
          }}
        >
          {positionsLabel}
        </button>
        <button
          data-screen="people"
          data-sub="timetable"
          className={tab === 'timetable' ? 'on' : ''}
          onClick={() => {
            const url = new URL(window.location.href);
            url.searchParams.set('tab', 'timetable');
            window.history.pushState({}, '', url);
          }}
        >
          {timetableLabel}
        </button>
      </div>

      {tab === 'positions' && <PositionsPanel pageContract={pageContract} />}
      {tab === 'timetable' && <TimetablePanel pageContract={pageContract} />}
    </section>
  );
}
