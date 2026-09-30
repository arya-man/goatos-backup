import { AdminShell } from "@/components/admin-shell";
import { NotFoundView } from "@/components/app/not-found-view";
import { PageRoot } from "@/components/app/page-root";
import { shellCopy } from "@/lib/admin-ui-contract";
import { getAdminWebBootstrap } from "@/lib/api/server";

/**
 * Root 404, rendered INSIDE the admin shell: an unknown path is a mistyped admin URL far more
 * often than not, and the sidebar and top bar are what let the reader recover. The body is the
 * template NotFoundView (J2B P2-14: it was a small StatePanel card with a tiny icon). The shell
 * fetches the bootstrap contract itself; the copy here is that contract's chrome copy.
 * (A catch-all route segment would give the same result but breaks Next's typed-routes validator.)
 */
export default async function RootNotFound() {
  const bootstrap = await getAdminWebBootstrap();
  const t = (key: string) => (bootstrap.ok ? shellCopy(bootstrap.data, key) : "");
  return (
    <AdminShell>
      <PageRoot>
        <NotFoundView title={t("not_found.title")} body={t("not_found.body")} homeLabel={t("not_found.home")} />
      </PageRoot>
    </AdminShell>
  );
}
