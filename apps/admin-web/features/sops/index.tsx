import { redirect } from "next/navigation";
import { ErrorPanel, PageHeader } from "@/components/admin-primitives";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getSOP, listSOPs } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { SopBuilder, type SopBuilderProps, type SopCatalogRow } from "./builder/sop-builder";
import type { ServerVersionFacts } from "./builder/model";

export async function SopsPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const selectedID = one(searchParams, "sop_id");
  const sops = await listSOPs({ limit: 100 });
  const authError = firstAuthRequiredError(sops);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  if (!sops.ok) {
    return (
      <>
        <Header />
        <ErrorPanel error={sops.error} />
      </>
    );
  }

  const rows: SopCatalogRow[] = sops.data.items.map((item) => ({
    sop_id: item.sop_id,
    code: item.code,
    name: item.name,
    status: item.status,
  }));

  const selectedId = selectedID ?? rows.find((r) => r.code === "shifting")?.sop_id ?? rows[0]?.sop_id ?? null;
  const detail = selectedId ? await getSOP(selectedId) : null;
  const secondAuthError = firstAuthRequiredError(detail);
  if (secondAuthError) redirect(INTERNAL_LOGIN_PATH);

  let selected: SopCatalogRow | null = null;
  let selectedFormDsl: unknown = null;
  let versionFacts: ServerVersionFacts = {
    sopId: null,
    sopVersionId: null,
    versionLabel: null,
    status: null,
    rowVersion: null,
    validationValid: null,
  };

  if (detail?.ok) {
    const sop = detail.data.sop;
    selected = { sop_id: sop.sop_id, code: sop.code, name: sop.name, status: sop.status };
    const version = detail.data.latest_version ?? null;
    if (version) {
      selectedFormDsl = version.form_dsl ?? null;
      versionFacts = {
        sopId: sop.sop_id,
        sopVersionId: version.sop_version_id,
        versionLabel: version.version_label,
        status: version.status,
        rowVersion: version.row_version,
        validationValid: version.validation_report?.valid ?? null,
      };
    }
  }

  const props: SopBuilderProps = { sops: rows, selected, selectedFormDsl, versionFacts };

  return (
    <>
      <Header />
      {detail && !detail.ok ? <ErrorPanel error={detail.error} /> : null}
      <SopBuilder {...props} />
    </>
  );
}

function Header() {
  return (
    <PageHeader
      eyebrow="SOP Builder"
      title="SOP Builder"
      description="Versioned form DSL, form logic, task flow, Android preview, dry-run validation, and publish controls for Shifting and later SOP families."
    />
  );
}
