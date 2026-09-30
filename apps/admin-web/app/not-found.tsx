import Button from "@mui/material/Button";

import Link from "@/components/no-prefetch-link";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/app/page-header";
import { PageRoot } from "@/components/app/page-root";
import { StatePanel } from "@/components/app/state-panel";
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
      <PageRoot>
        <PageHeader title={t("not_found.title")} crumbs={[{ label: t("not_found.crumb_home"), href: "/" }, { label: t("not_found.crumb") }]} />
        <StatePanel
          page={false}
          role="status"
          tone="primary"
          icon="solar:home-angle-bold-duotone"
          titleComponent="h2"
          title="404"
          body={t("not_found.body")}
          actions={
            <Button variant="contained" color="primary" component={Link} href="/">
              {t("not_found.home")}
            </Button>
          }
        />
      </PageRoot>
    </AdminShell>
  );
}
