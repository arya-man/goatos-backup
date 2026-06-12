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
    <header className="mb-5 flex flex-col gap-2 border-b border-[#334155] pb-5 md:flex-row md:items-end md:justify-between">
      <div>
        <div className="text-xs font-semibold uppercase text-[#14f1d9]">{eyebrow}</div>
        <h1 className="mt-1 text-2xl font-bold text-white">{title}</h1>
        <p className="mt-2 max-w-4xl text-sm leading-6 text-[#8899AA]">{description}</p>
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
    <section className="rounded-xl border border-[#334155] bg-[#1A1D24]">
      <div className="flex flex-col gap-2 border-b border-[#334155] px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-sm font-bold text-white">{title}</h2>
          {description ? <p className="mt-1 text-sm text-[#8899AA]">{description}</p> : null}
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
  if (error.kind === "missing_config" || error.code === "missing_config") {
    return (
      <div className="rounded-lg border border-dashed border-[#334155] bg-[#11151C] px-4 py-6 text-sm text-[#8899AA]">
        <div className="font-semibold text-[#c7d1dc]">Local API configuration needed</div>
        <p className="mt-1">
          Start this server with the local backend URL, tenant, and bearer token to load live rows.
        </p>
      </div>
    );
  }

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

export function ActionNotice({ status, message }: { status?: string; message?: string }) {
  if (!status || !message) return null;
  const isSuccess = status === "success";
  return (
    <div className={`mb-4 rounded-md border p-3 text-sm ${isSuccess ? "border-[#1f8f65] bg-[#102018] text-[#b7f7ce]" : "border-[#7f1d1d] bg-[#1d1214] text-[#fecaca]"}`}>
      {message}
    </div>
  );
}

export function EmptyPanel({ message }: { message: string }) {
  return <div className="rounded-lg border border-dashed border-[#334155] bg-[#11151C] px-4 py-8 text-center text-sm text-[#8899AA]">{message}</div>;
}

export function LoadingBlock({ label = "Loading" }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-[#334155] bg-[#1A1D24] px-4 py-3 text-sm text-[#c7d1dc]">
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
      className="inline-flex h-9 items-center gap-2 rounded-lg border border-[#334155] px-3 text-sm text-[#f8fafc] hover:bg-[#22262E]"
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
        <div key={label} className="rounded-lg border border-[#334155] bg-[#11151C] p-3">
          <dt className="text-xs uppercase text-[#8899AA]">{label}</dt>
          <dd className="mt-1 break-words text-[#f8fafc]">{value ?? "—"}</dd>
        </div>
      ))}
    </dl>
  );
}

export function FormField({
  name,
  label,
  defaultValue,
  placeholder,
  type = "text",
  required = false,
  min,
  max,
}: {
  name: string;
  label: string;
  defaultValue?: string;
  placeholder?: string;
  type?: string;
  required?: boolean;
  min?: string;
  max?: string;
}) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <input
        name={name}
        type={type}
        required={required}
        min={min}
        max={max}
        defaultValue={defaultValue}
        placeholder={placeholder}
        className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      />
    </label>
  );
}

export function FormSelect({
  name,
  label,
  defaultValue,
  options,
  required = false,
  emptyLabel = "Any",
}: {
  name: string;
  label: string;
  defaultValue?: string;
  options: string[];
  required?: boolean;
  emptyLabel?: string;
}) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <select
        name={name}
        defaultValue={defaultValue ?? ""}
        required={required}
        className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      >
        <option value="">{emptyLabel}</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </label>
  );
}

export function FormTextArea({
  name,
  label,
  defaultValue,
  placeholder,
  required = false,
  rows = 3,
}: {
  name: string;
  label: string;
  defaultValue?: string;
  placeholder?: string;
  required?: boolean;
  rows?: number;
}) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <textarea
        name={name}
        required={required}
        rows={rows}
        defaultValue={defaultValue}
        placeholder={placeholder}
        className="mt-1 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 py-2 text-sm text-white outline-none focus:border-[#14f1d9]"
      />
    </label>
  );
}
