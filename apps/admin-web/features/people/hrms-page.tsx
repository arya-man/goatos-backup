'use client';

import { VaccinationOperatorsScreen } from './vaccination-operators-screen';
import { type RouteSearchParams } from '@/lib/search-params';
import { type AdminUiPageContract } from '@/lib/admin-ui-contract';

interface HRMSPageProps {
  searchParams: RouteSearchParams;
  tab: string;
  pageContract?: AdminUiPageContract;
}

export function HRMSPage({ pageContract }: HRMSPageProps) {
  return <VaccinationOperatorsScreen pageContract={pageContract} />;
}
