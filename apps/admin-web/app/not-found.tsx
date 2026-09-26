import { Compass } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/app/page-header";
import { shellCopy } from "@/lib/admin-ui-contract";
import { getAdminWebBootstrap } from "@/lib/api/server";

/**
 * Root 404, rendered INSIDE the admin shell: an unknown path is a mistyped admin URL far more
 * often than not, and the sidebar, top bar and breadcrumb are what let the reader recover. The
 * shell fetches the bootstrap contract itself; the copy here is that contract's chrome copy.
 * (A catch-all route segment would give the same result but breaks Next's typed-routes validator.)
 */
export default async function RootNotFound() {
  const bootstrap = await getAdminWebBootstrap();
  const t = (key: string) => (bootstrap.ok ? shellCopy(bootstrap.data, key) : "");
  return (
    <AdminShell>
      <div className="kit-page">
        <PageHeader title={t("not_found.title")} crumbs={[{ label: t("not_found.crumb_home"), href: "/" }, { label: t("not_found.crumb") }]} />
        <section className="kit-state kit-state-inline" role="status">
          <span className="kit-state-icon" aria-hidden="true">
            <Compass />
          </span>
          <h2 className="kit-state-title">404</h2>
          <p className="kit-state-body">{t("not_found.body")}</p>
          <Link href="/" className="btn primary">
            {t("not_found.home")}
          </Link>
        </section>
      </div>
    </AdminShell>
  );
}
