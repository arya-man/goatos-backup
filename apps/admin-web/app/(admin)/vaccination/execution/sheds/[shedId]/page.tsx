import { ShedExecutionDetailPage } from "@/features/vaccination-execution";

export const dynamic = "force-dynamic";

export default async function Page({ params }: { params: Promise<{ shedId: string }> }) {
  const { shedId } = await params;
  return <ShedExecutionDetailPage shedId={shedId} />;
}
