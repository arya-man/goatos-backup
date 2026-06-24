import { SopLibrary, isVaccinationSop, toSopView, type SopCardView } from "@/features/sops";
import { getSop, isAuthRequiredError, listSops } from "@/lib/api/server";

export const dynamic = "force-dynamic";

// Admin / Data Ops / SOP Library (route reopened as the new SOP Library — not the old SOP/tasks UI).
// Lists real `/admin/sops` definitions, then fetches each SOP's latest version to derive the card
// facets (domain / trigger / steps / gates) from real form_dsl + proof_policy. No mock rows.
export default async function Page() {
  const listed = await listSops({ limit: 200 });

  if (!listed.ok) {
    if (isAuthRequiredError(listed.error)) {
      return <SopLibrary sops={[]} authRequired />;
    }
    return <SopLibrary sops={[]} error={{ code: listed.error.code, message: listed.error.message }} />;
  }

  // SCOPE LOCK: the visible /sops slice is vaccination only. Filter the real API result to vaccination
  // SOPs BEFORE fetching detail — shifting and every non-vaccination SOP are hidden from this surface,
  // and we do not waste detail fetches on hidden rows. No backend/data change; pure frontend scoping.
  const defs = listed.data.items.filter((def) => isVaccinationSop(def.code, def.name));
  // Per-SOP detail fetch (bounded by the list limit — an admin authoring set, not a herd scan). Each
  // returns the latest version; a failed detail still renders the definition card with null version.
  const details = await Promise.all(defs.map((def) => getSop(def.sop_id)));

  const sops: SopCardView[] = defs.map((def, i) => {
    const detail = details[i];
    const version = detail.ok ? (detail.data.latest_version ?? null) : null;
    return toSopView(def, version);
  });

  return <SopLibrary sops={sops} />;
}
