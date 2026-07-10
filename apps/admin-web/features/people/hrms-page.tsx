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
          Position &amp; Coverage
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
          Timetable
        </button>
      </div>

      {tab === 'positions' && <PositionsPanel pageContract={pageContract} />}
      {tab === 'timetable' && <TimetablePanel pageContract={pageContract} />}
    </section>
  );
}
