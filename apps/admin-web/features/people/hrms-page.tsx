'use client';

import { VaccinationOperatorsScreen } from './vaccination-operators-screen';
import { one, type RouteSearchParams } from '@/lib/search-params';
import { type AdminUiPageContract } from '@/lib/admin-ui-contract';

interface HRMSPageProps {
  searchParams: RouteSearchParams;
  tab: string;
  pageContract?: AdminUiPageContract;
}

export function HRMSPage({ searchParams, pageContract }: HRMSPageProps) {
  const park = one(searchParams, "park");
  const initialParkId = park && park !== "all" ? park : undefined;
  return <VaccinationOperatorsScreen initialParkId={initialParkId} pageContract={pageContract} />;
}
