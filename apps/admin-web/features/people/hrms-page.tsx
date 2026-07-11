'use client';

import { useRouter, useSearchParams } from 'next/navigation';
import { PositionsPanel } from './positions-panel';
import { TimetablePanel } from './timetable-panel';
import { type RouteSearchParams } from '@/lib/search-params';
import { type AdminUiPageContract } from '@/lib/admin-ui-contract';

interface HRMSPageProps {
  searchParams: RouteSearchParams;
  tab: string;
  pageContract?: AdminUiPageContract;
}

export function HRMSPage({ tab: initialTab, pageContract }: HRMSPageProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const activeTab = searchParams.get('tab') ?? initialTab;

  const handleTabChange = (newTab: string) => {
    const params = new URLSearchParams(searchParams);
    params.set('tab', newTab);
    router.push(`?${params.toString()}`);
  };

  // Render tabs from pageContract.tables, with fallback to defaults
  const positionsLabel = pageContract?.tables?.find((t) => t.id === 'positions')?.title ?? 'Position & Coverage';
  const timetableLabel = pageContract?.tables?.find((t) => t.id === 'timetable')?.title ?? 'Timetable';

  return (
    <section className="screen on" data-screen="people">
      <div className="subtabs">
        <button
          data-screen="people"
          data-sub="positions"
          className={activeTab === 'positions' ? 'on' : ''}
          onClick={() => handleTabChange('positions')}
        >
          {positionsLabel}
        </button>
        <button
          data-screen="people"
          data-sub="timetable"
          className={activeTab === 'timetable' ? 'on' : ''}
          onClick={() => handleTabChange('timetable')}
        >
          {timetableLabel}
        </button>
      </div>

      {activeTab === 'positions' && <PositionsPanel pageContract={pageContract} />}
      {activeTab === 'timetable' && <TimetablePanel pageContract={pageContract} />}
    </section>
  );
}
