import MuiSkeleton from "@mui/material/Skeleton";
import { KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";
import "@/layouts/mesha-layout.css";
import "@/layouts/shell-skeleton.css";

/**
 * The shell, drawn before the bootstrap contract arrives. Same geometry as the MUI Minimal
 * DashboardLayout the shell renders (layouts/dashboard): a 300px nav column (88px when the reader
 * keeps the mini rail, from `html[data-nav-rail]`, set server-side from the rail cookie), a
 * 64/72px header, and the page column as `.main.msh-content > .wrap.msh-wrap`, so the first paint
 * does not jump when the real shell hydrates. The nav is hidden below 1200px, like the template.
 * No copy of its own beyond the wordmark tile.
 */
export function ShellSkeleton({ children }: { children?: React.ReactNode }) {
  return (
    <div className="shell-skeleton msh-skel" aria-busy="true">
      <aside className="msh-skel-nav" aria-hidden="true">
        <div className="msh-skel-logo">
          <span className="msh-skel-mark">मे</span>
        </div>
        {[6, 8].map((rows, group) => (
          <div key={group} className="msh-skel-group">
            <div className="msh-skel-subheader">
              <MuiSkeleton variant="text" width={group ? 56 : 64} />
            </div>
            {Array.from({ length: rows }, (_, i) => (
              <div key={i} className="msh-skel-item">
                <MuiSkeleton variant="rounded" width={24} height={24} />
                <span className="msh-skel-label">
                  <MuiSkeleton variant="text" width={(group ? 80 : 90) + (i % (group ? 4 : 3)) * (group ? 16 : 18)} />
                </span>
              </div>
            ))}
          </div>
        ))}
      </aside>
      <div className="msh-skel-col">
        <div className="msh-skel-header" aria-hidden="true">
          <MuiSkeleton variant="circular" width={40} height={40} />
          <MuiSkeleton variant="circular" width={40} height={40} />
          <MuiSkeleton variant="circular" width={40} height={40} />
        </div>
        <main className="main msh-content msh-skel-main">
          <div className="wrap msh-wrap">{children ?? <GenericPageSkeleton />}</div>
        </main>
      </div>
    </div>
  );
}

/** The page-column skeleton for a route that has no loading.tsx of its own: header, KPI deck, table card. */
export function GenericPageSkeleton() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={4} />
      <TableSkeleton columns={5} rows={8} />
    </PageSkeleton>
  );
}
