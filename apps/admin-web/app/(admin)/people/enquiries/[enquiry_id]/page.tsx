import { EnquiryPage } from "@/features/discipline";
import { requireAdminWebPageContract } from "@/lib/api/server";

export const dynamic = "force-dynamic";

// One enquiry's report (maintainer decisions 2026-09-30).
export default async function Page({ params }: { params: Promise<{ enquiry_id: string }> }) {
  const [{ enquiry_id }, pageContract] = await Promise.all([params, requireAdminWebPageContract("people-enquiry")]);
  return <EnquiryPage enquiryId={enquiry_id} pageContract={pageContract} />;
}
