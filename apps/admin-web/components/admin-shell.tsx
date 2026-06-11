import Link from "next/link";
import {
  Activity,
  AlertTriangle,
  BadgeCheck,
  DatabaseZap,
  FileSearch,
  Home,
  Search,
} from "lucide-react";

const navItems = [
  { href: "/", label: "Overview", icon: Home },
  { href: "/herd", label: "Herd Search", icon: Search },
  { href: "/counts", label: "Identity Counts", icon: Activity },
  { href: "/data-quality", label: "Data Quality", icon: FileSearch },
  { href: "/import-review", label: "Import Review", icon: DatabaseZap },
  { href: "/herd", label: "Goat Passport", icon: BadgeCheck },
] as const;

export function AdminShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-[#0f1115] text-[#f8fafc]">
      <div className="flex min-h-screen">
        <aside className="hidden w-[236px] shrink-0 border-r border-[#293241] bg-[#151820] px-4 py-5 lg:block">
          <Link href="/" className="block border-b border-[#293241] pb-5">
            <div className="text-xs font-semibold uppercase text-[#14f1d9]">Goat OS</div>
            <div className="mt-1 text-xl font-semibold">Admin</div>
            <div className="mt-1 text-xs text-[#93a4b8]">Identity operations</div>
          </Link>
          <nav className="mt-5 space-y-1" aria-label="Goat OS admin routes">
            {navItems.map((item) => {
              const Icon = item.icon;
              return (
                <Link
                  key={`${item.href}-${item.label}`}
                  href={item.href}
                  className="flex h-10 items-center gap-3 rounded-md px-3 text-sm text-[#c7d1dc] hover:bg-[#202631] hover:text-white"
                >
                  <Icon className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
                  <span className="truncate">{item.label}</span>
                </Link>
              );
            })}
          </nav>
          <div className="mt-6 rounded-md border border-[#293241] bg-[#10141b] p-3 text-xs text-[#93a4b8]">
            <div className="flex items-center gap-2 font-medium text-[#f8fafc]">
              <AlertTriangle className="h-4 w-4 text-[#facc15]" aria-hidden="true" />
              Phase 1
            </div>
            <p className="mt-2 leading-5">Read-only admin surface backed by server-side Goat OS APIs.</p>
          </div>
        </aside>
        <div className="min-w-0 flex-1">
          <header className="border-b border-[#293241] bg-[#151820] px-4 py-3 lg:hidden">
            <div className="text-xs font-semibold uppercase text-[#14f1d9]">Goat OS Admin</div>
            <nav className="mt-3 flex gap-2 overflow-x-auto" aria-label="Goat OS admin mobile routes">
              {navItems.map((item) => (
                <Link
                  key={`${item.href}-${item.label}-mobile`}
                  href={item.href}
                  className="whitespace-nowrap rounded-md border border-[#293241] px-3 py-2 text-xs text-[#c7d1dc]"
                >
                  {item.label}
                </Link>
              ))}
            </nav>
          </header>
          <main className="mx-auto w-full max-w-[1440px] px-4 py-5 sm:px-6 lg:px-8">{children}</main>
        </div>
      </div>
    </div>
  );
}
