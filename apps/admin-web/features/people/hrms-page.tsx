'use client';

import { PositionsPanel } from './positions-panel';
import { TimetablePanel } from './timetable-panel';
import { type RouteSearchParams } from '@/lib/search-params';

interface HRMSPageProps {
  searchParams: RouteSearchParams;
  tab: string;
}

export function HRMSPage({ tab }: HRMSPageProps) {
  return (
    <section className="screen" data-screen="people">
      <div className="subtabs">
        <button
          data-screen="people"
          data-sub="positions"
          className={tab === 'positions' ? 'on' : ''}
        >
          Position &amp; Coverage
        </button>
        <button
          data-screen="people"
          data-sub="timetable"
          className={tab === 'timetable' ? 'on' : ''}
        >
          Timetable
        </button>
      </div>

      {tab === 'positions' && <PositionsPanel />}
      {tab === 'timetable' && <TimetablePanel />}
    </section>
  );
}
