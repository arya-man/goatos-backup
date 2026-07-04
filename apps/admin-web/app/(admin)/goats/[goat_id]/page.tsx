import { GoatPassportPage } from "@/features/goat-passport";
import { VaccinationPassportSection } from "@/features/preventive-care-vaccination";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ params, searchParams }: { params: Promise<{ goat_id: string }>; searchParams: Promise<RouteSearchParams> }) {
  const { goat_id } = await params;
  const pageContract = await requireAdminWebPageContract("goat-passport");
  return (
    <>
      <GoatPassportPage goatId={goat_id} searchParams={await searchParams} pageContract={pageContract} />
      <div className="mt-4">
        <VaccinationPassportSection goatId={goat_id} pageContract={pageContract} />
      </div>
    </>
  );
}
