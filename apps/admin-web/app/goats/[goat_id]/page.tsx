import { GoatPassportPage } from "@/features/goat-passport";

export const dynamic = "force-dynamic";

export default async function Page({ params }: { params: Promise<{ goat_id: string }> }) {
  const { goat_id } = await params;
  return <GoatPassportPage goatId={goat_id} />;
}
