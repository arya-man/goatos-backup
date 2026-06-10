import {
  Activity,
  BadgeCheck,
  DatabaseZap,
  FileSearch,
  GitBranch,
  Search,
} from "lucide-react";
import { getApiClientSmokeConfig } from "@/lib/api-client-smoke";

const navItems = [
  { label: "Herd Search", icon: Search },
  { label: "Goat Passport", icon: BadgeCheck },
  { label: "Import Runs", icon: DatabaseZap },
  { label: "Dirty Data Review", icon: FileSearch },
  { label: "Corrections", icon: GitBranch },
  { label: "Analytics Counts", icon: Activity },
] as const;

const emptyStates = [
  { label: "Herd", value: "No herd loaded" },
  { label: "Passport", value: "No goat selected" },
  { label: "Review", value: "No queue selected" },
  { label: "Imports", value: "No import selected" },
];

export default function AdminReadinessShell() {
  getApiClientSmokeConfig();

  return (
    <main className="min-h-screen bg-[#0F1115] text-white">
      <div className="flex min-h-screen">
        <aside className="hidden w-[220px] border-r border-[#334155] bg-[#1A1D24] px-4 py-5 md:block">
          <div className="mb-8">
            <div className="text-xs font-semibold uppercase tracking-[0.18em] text-[#14F1D9]">Goat OS</div>
            <h1 className="mt-2 text-xl font-bold">Admin</h1>
            <p className="mt-1 text-xs text-[#8899AA]">Operations console</p>
          </div>
          <nav className="space-y-2" aria-label="Goat OS admin tabs">
            {navItems.map((item) => {
              const Icon = item.icon;
              return (
                <button
                  key={item.label}
                  type="button"
                  disabled
                  className="flex h-11 w-full cursor-not-allowed items-center gap-3 rounded-lg border border-[#293241] bg-[#151820] px-3 text-left text-sm text-[#7F8DA1] opacity-70"
                  aria-disabled="true"
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
                  Goat Passport
                </p>
                <h2 className="mt-2 text-3xl font-bold tracking-normal">Mesha Herd Operations</h2>
                <p className="mt-2 max-w-3xl text-sm leading-6 text-[#AAB7C4]">Identity, imports, reviews, and herd counters in one console.</p>
              </div>
              <div className="rounded-md border border-[#334155] bg-[#1A1D24] px-4 py-3 text-sm text-[#AAB7C4]">
                <div className="text-[#8899AA]">Workspace</div>
                <div className="mt-1 font-semibold text-white">Mesha</div>
                <div className="mt-2 text-[#8899AA]">No active selection</div>
              </div>
            </div>
          </header>

          <div className="grid gap-4 lg:grid-cols-[1.1fr_0.9fr]">
            <section className="rounded-md border border-[#334155] bg-[#1A1D24] p-5">
              <h3 className="text-lg font-semibold">Work Areas</h3>
              <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {navItems.map((item) => {
                  const Icon = item.icon;
                  return (
                    <div key={item.label} className="rounded-md border border-[#293241] bg-[#12151B] p-4">
                      <div className="flex items-center justify-between gap-3">
                        <Icon className="h-5 w-5 text-[#14F1D9]" aria-hidden="true" />
                        <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#8899AA]">
                          Locked
                        </span>
                      </div>
                      <div className="mt-4 font-semibold">{item.label}</div>
                      <div className="mt-1 text-sm text-[#8899AA]">No active view</div>
                    </div>
                  );
                })}
              </div>
            </section>

            <section className="rounded-md border border-[#334155] bg-[#1A1D24] p-5">
              <h3 className="text-lg font-semibold">Current View</h3>
              <div className="mt-4 space-y-3">
                {emptyStates.map((item) => (
                  <div key={item.label} className="rounded-md border border-[#293241] bg-[#12151B] px-4 py-3">
                    <div className="text-xs uppercase tracking-[0.14em] text-[#14F1D9]/70">{item.label}</div>
                    <div className="mt-1 text-sm text-[#C7D1DC]">{item.value}</div>
                  </div>
                ))}
              </div>
            </section>
          </div>
        </section>
      </div>
    </main>
  );
}
