import Link from "next/link";
import { AlertCircle, ArrowRight, Loader2 } from "lucide-react";
import type { ApiUiError } from "@/lib/api/server";

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow: string;
  title: string;
  description: string;
  actions?: React.ReactNode;
}) {
  return (
    <header className="mb-5 flex flex-col gap-3 border-b border-[#293241] pb-5 md:flex-row md:items-end md:justify-between">
      <div>
        <div className="text-xs font-semibold uppercase text-[#14f1d9]">{eyebrow}</div>
        <h1 className="mt-1 text-2xl font-semibold text-white sm:text-3xl">{title}</h1>
        <p className="mt-2 max-w-4xl text-sm leading-6 text-[#aab7c4]">{description}</p>
      </div>
      {actions ? <div className="shrink-0">{actions}</div> : null}
    </header>
  );
}

export function Panel({
  title,
  description,
  children,
  action,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <section className="rounded-md border border-[#293241] bg-[#151820]">
      <div className="flex flex-col gap-2 border-b border-[#293241] px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-base font-semibold text-white">{title}</h2>
          {description ? <p className="mt-1 text-sm text-[#93a4b8]">{description}</p> : null}
        </div>
        {action}
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}

export function StatPill({ label, value, tone = "neutral" }: { label: string; value: React.ReactNode; tone?: "neutral" | "good" | "warn" }) {
  const tones = {
    neutral: "border-[#334155] text-[#c7d1dc]",
    good: "border-[#1f8f65] text-[#7dd3a7]",
    warn: "border-[#a16207] text-[#facc15]",
  };
  return (
    <div className={`rounded-md border px-3 py-2 ${tones[tone]}`}>
      <div className="text-xs uppercase text-[#93a4b8]">{label}</div>
      <div className="mt-1 text-lg font-semibold">{value}</div>
    </div>
  );
}

export function ErrorPanel({ error }: { error: ApiUiError }) {
  return (
    <div className="rounded-md border border-[#7f1d1d] bg-[#1d1214] p-4 text-sm">
      <div className="flex items-start gap-3">
        <AlertCircle className="mt-0.5 h-5 w-5 shrink-0 text-[#f87171]" aria-hidden="true" />
        <div>
          <div className="font-semibold text-[#fecaca]">
            {error.status ? `${error.status} ` : ""}
            {error.code ?? error.kind}
          </div>
          <p className="mt-1 text-[#f3b7b7]">{error.message}</p>
          {error.traceId ? <p className="mt-2 font-mono text-xs text-[#fca5a5]">trace {error.traceId}</p> : null}
        </div>
      </div>
    </div>
  );
}

export function EmptyPanel({ message }: { message: string }) {
  return <div className="rounded-md border border-dashed border-[#334155] px-4 py-8 text-center text-sm text-[#93a4b8]">{message}</div>;
}

export function LoadingBlock({ label = "Loading" }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 rounded-md border border-[#293241] bg-[#151820] px-4 py-3 text-sm text-[#c7d1dc]">
      <Loader2 className="h-4 w-4 animate-spin text-[#14f1d9]" aria-hidden="true" />
      {label}
    </div>
  );
}

export function NextPageLink({ href, label = "Load more" }: { href: string | null; label?: string }) {
  if (!href) return null;
  return (
    <Link
      href={href}
      className="inline-flex h-9 items-center gap-2 rounded-md border border-[#334155] px-3 text-sm text-[#f8fafc] hover:bg-[#202631]"
    >
      {label}
      <ArrowRight className="h-4 w-4" aria-hidden="true" />
    </Link>
  );
}

export function Mono({ children }: { children: React.ReactNode }) {
  return <span className="font-mono text-xs text-[#c7d1dc]">{children}</span>;
}

export function ValueList({ values }: { values: Array<[string, React.ReactNode]> }) {
  return (
    <dl className="grid gap-2 text-sm sm:grid-cols-2 xl:grid-cols-3">
      {values.map(([label, value]) => (
        <div key={label} className="rounded-md border border-[#293241] bg-[#10141b] p-3">
          <dt className="text-xs uppercase text-[#93a4b8]">{label}</dt>
          <dd className="mt-1 break-words text-[#f8fafc]">{value ?? "—"}</dd>
        </div>
      ))}
    </dl>
  );
}
