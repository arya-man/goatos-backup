import {
  Activity,
  AlertTriangle,
  BadgeCheck,
  DatabaseZap,
  FileSearch,
  GitBranch,
  Search,
} from "lucide-react";
import { getApiClientSmokeConfig } from "@/lib/api-client-smoke";

const navItems = [
  { label: "Herd Search", icon: Search, status: "Next slice", enabled: false },
  { label: "Goat Passport", icon: BadgeCheck, status: "Next slice", enabled: false },
  { label: "Import Runs", icon: DatabaseZap, status: "Next slice", enabled: false },
  { label: "Dirty Data Review", icon: FileSearch, status: "Next slice", enabled: false },
  { label: "Corrections", icon: GitBranch, status: "Next slice", enabled: false },
  { label: "Analytics Counts", icon: Activity, status: "Next slice", enabled: false },
] as const;

const readinessItems = [
  "Generated OpenAPI client package is the admin-web data path.",
  "Bearer auth is expected from the Goat OS backend; dev headers are not used by the web shell.",
  "Legacy BigQuery and Sheets API routes have been removed from the executable app.",
  "Tabs are placeholders only; this slice does not fetch herd data or call incomplete endpoints.",
];

export default function AdminReadinessShell() {
  const smoke = getApiClientSmokeConfig();

  return (
    <main className="min-h-screen bg-[#0F1115] text-white">
      <div className="flex min-h-screen">
        <aside className="hidden w-[220px] border-r border-[#334155] bg-[#1A1D24] px-4 py-5 md:block">
          <div className="mb-8">
            <div className="text-xs font-semibold uppercase tracking-[0.18em] text-[#14F1D9]">Goat OS</div>
            <h1 className="mt-2 text-xl font-bold">Admin</h1>
            <p className="mt-1 text-xs text-[#8899AA]">Phase 1 readiness</p>
          </div>
          <nav className="space-y-2" aria-label="Phase 1 admin tabs">
            {navItems.map((item) => {
              const Icon = item.icon;
              return (
                <button
                  key={item.label}
                  type="button"
                  disabled={!item.enabled}
                  className="flex h-11 w-full cursor-not-allowed items-center gap-3 rounded-lg border border-[#293241] bg-[#151820] px-3 text-left text-sm text-[#7F8DA1] opacity-70"
                  aria-disabled={!item.enabled}
                  title={`${item.label} will be wired in a later Phase 1 frontend slice`}
                >
                  <Icon className="h-4 w-4 shrink-0 text-[#14F1D9]/60" aria-hidden="true" />
                  <span className="truncate">{item.label}</span>
                </button>
              );
            })}
          </nav>
        </aside>

        <section className="flex-1 px-5 py-5 sm:px-8 lg:px-10">
          <header className="mb-8 border-b border-[#334155] pb-5">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[#14F1D9]">
                  Goat Passport Foundation
                </p>
                <h2 className="mt-2 text-3xl font-bold tracking-normal">Admin Web Readiness Shell</h2>
                <p className="mt-2 max-w-3xl text-sm leading-6 text-[#AAB7C4]">
                  This is the buildable Phase 1 shell for the real admin tabs. Data fetching is intentionally
                  disabled until the next slice wires screens through the generated Goat OS API client.
                </p>
              </div>
              <div className="rounded-md border border-[#334155] bg-[#1A1D24] px-4 py-3 text-sm">
                <div className="text-[#8899AA]">API base</div>
                <div className="mt-1 max-w-[300px] truncate font-mono text-[#14F1D9]">{smoke.baseUrl}</div>
                <div className="mt-2 text-[#8899AA]">
                  Token env: <span className="text-white">{smoke.hasBearerToken ? "configured" : "not set"}</span>
                </div>
              </div>
            </div>
          </header>

          <div className="grid gap-4 lg:grid-cols-[1.1fr_0.9fr]">
            <section className="rounded-md border border-[#334155] bg-[#1A1D24] p-5">
              <h3 className="text-lg font-semibold">Phase 1 Tabs</h3>
              <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {navItems.map((item) => {
                  const Icon = item.icon;
                  return (
                    <div key={item.label} className="rounded-md border border-[#293241] bg-[#12151B] p-4">
                      <div className="flex items-center justify-between gap-3">
                        <Icon className="h-5 w-5 text-[#14F1D9]" aria-hidden="true" />
                        <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#8899AA]">
                          Disabled
                        </span>
                      </div>
                      <div className="mt-4 font-semibold">{item.label}</div>
                      <div className="mt-1 text-sm text-[#8899AA]">{item.status}</div>
                    </div>
                  );
                })}
              </div>
            </section>

            <section className="rounded-md border border-[#334155] bg-[#1A1D24] p-5">
              <div className="flex items-center gap-2">
                <AlertTriangle className="h-5 w-5 text-[#14F1D9]" aria-hidden="true" />
                <h3 className="text-lg font-semibold">Readiness Gates</h3>
              </div>
              <ul className="mt-4 space-y-3">
                {readinessItems.map((item) => (
                  <li key={item} className="rounded-md border border-[#293241] bg-[#12151B] px-4 py-3 text-sm text-[#C7D1DC]">
                    {item}
                  </li>
                ))}
              </ul>
            </section>
          </div>
        </section>
      </div>
    </main>
  );
}
