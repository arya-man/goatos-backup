import { redirect } from "next/navigation";
import { AlertTriangle, CheckCircle2, KeyRound, Smartphone, UserCog, Users } from "lucide-react";
import { ActionNotice, EmptyPanel, ErrorPanel, PageHeader, Panel, StatPill } from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { formatLabel } from "@/lib/display-utils";
import { dash, dateTime, joinParts, shortId } from "@/lib/format";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import {
  firstAuthRequiredError,
  getOperator,
  listOperatorDevices,
  listOperatorGrants,
  listOperators,
  listOperatorSourceCandidates,
  type DeviceListResponse,
  type GrantListResponse,
  type OperatorListResponse,
  type OperatorResponse,
  type SourceCandidateListResponse,
} from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import {
  activateOperatorAction,
  deactivateOperatorAction,
  mapSourceCandidateAction,
  rejectSourceCandidateAction,
} from "./actions";

type OperatorRow = OperatorListResponse["items"][number];
type GrantRow = GrantListResponse["items"][number];
type DeviceRow = DeviceListResponse["items"][number];
type CandidateRow = SourceCandidateListResponse["items"][number];

export async function OperatorsPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const selectedID = one(searchParams, "operator_id");
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");
  const filters = {
    limit: 100,
    search: one(searchParams, "search"),
    status: one(searchParams, "status"),
    role_hint: one(searchParams, "role_hint"),
  };

  const operators = await listOperators(filters);
  const authError = firstAuthRequiredError(operators);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  if (!operators.ok) {
    return (
      <>
        <Header />
        <ErrorPanel error={operators.error} />
      </>
    );
  }

  const selected = selectedID ?? operators.data.items[0]?.operator_id;
  const [selectedOperator, grants, devices, candidates] = await Promise.all([
    selected ? getOperator(selected) : Promise.resolve(null),
    selected ? listOperatorGrants(selected) : Promise.resolve(null),
    selected ? listOperatorDevices(selected) : Promise.resolve(null),
    listOperatorSourceCandidates({ limit: 25, status: "candidate" }),
  ]);

  const secondAuthError = firstAuthRequiredError(selectedOperator, grants, devices, candidates);
  if (secondAuthError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  return (
    <>
      <Header />
      <ActionNotice status={actionStatus} message={actionMessage} />
      <FilterBar filters={filters} />
      <OperatorsBody
        operators={operators.data.items}
        selected={selectedOperator?.ok ? selectedOperator.data : null}
        selectedError={selectedOperator && !selectedOperator.ok ? selectedOperator.error : null}
        grants={grants?.ok ? grants.data.items : []}
        grantsError={grants && !grants.ok ? grants.error : null}
        devices={devices?.ok ? devices.data.items : []}
        devicesError={devices && !devices.ok ? devices.error : null}
        candidates={candidates.ok ? candidates.data.items : []}
        candidatesError={!candidates.ok ? candidates.error : null}
        returnTo={returnPath(searchParams)}
      />
    </>
  );
}

function Header() {
  return (
    <PageHeader
      eyebrow="Operator Management"
      title="Operators"
      description="Roster, grants, capabilities, devices, and sanitized legacy source mappings for Android SOP execution."
    />
  );
}

function FilterBar({ filters }: { filters: { search?: string; status?: string; role_hint?: string } }) {
  return (
    <form className="mb-5 grid gap-3 rounded-xl border border-[#334155] bg-[#1A1D24] p-4 md:grid-cols-[minmax(0,1fr)_160px_160px_auto]" action="/operators">
      <label>
        <span className="text-xs uppercase text-[#93a4b8]">Search</span>
        <input
          name="search"
          defaultValue={filters.search}
          placeholder="Name or code"
          className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
        />
      </label>
      <label>
        <span className="text-xs uppercase text-[#93a4b8]">Status</span>
        <select
          name="status"
          defaultValue={filters.status ?? ""}
          className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
        >
          <option value="">Any</option>
          <option value="candidate">Candidate</option>
          <option value="active">Active</option>
          <option value="inactive">Inactive</option>
          <option value="suspended">Suspended</option>
          <option value="left">Left</option>
        </select>
      </label>
      <label>
        <span className="text-xs uppercase text-[#93a4b8]">Role Hint</span>
        <select
          name="role_hint"
          defaultValue={filters.role_hint ?? ""}
          className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
        >
          <option value="">Any</option>
          <option value="operator">Operator</option>
          <option value="park_head">Park head</option>
          <option value="verifier">Verifier</option>
          <option value="supervisor">Supervisor</option>
          <option value="admin">Admin</option>
          <option value="other">Other</option>
        </select>
      </label>
      <button
        type="submit"
        className="mt-5 inline-flex h-10 items-center justify-center rounded-lg border border-[#14F1D9]/50 px-4 text-sm font-bold text-[#14F1D9] hover:border-[#14F1D9] md:mt-6"
      >
        Apply
      </button>
    </form>
  );
}

function OperatorsBody({
  operators,
  selected,
  selectedError,
  grants,
  grantsError,
  devices,
  devicesError,
  candidates,
  candidatesError,
  returnTo,
}: {
  operators: OperatorRow[];
  selected: OperatorResponse | null;
  selectedError: Parameters<typeof ErrorPanel>[0]["error"] | null;
  grants: GrantRow[];
  grantsError: Parameters<typeof ErrorPanel>[0]["error"] | null;
  devices: DeviceRow[];
  devicesError: Parameters<typeof ErrorPanel>[0]["error"] | null;
  candidates: CandidateRow[];
  candidatesError: Parameters<typeof ErrorPanel>[0]["error"] | null;
  returnTo: string;
}) {
  const active = operators.filter((operator) => operator.status === "active").length;
  const withGrant = operators.filter((operator) => operator.grant_count > 0).length;
  const withDevice = operators.filter((operator) => operator.active_device_count > 0).length;
  const selectedOperator = selected?.operator ?? null;

  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Roster" value={operators.length} detail={`${active.toLocaleString("en-IN")} active`} icon={<Users size={18} />} />
        <StatCard label="Granted" value={withGrant} detail="active DB grants" icon={<KeyRound size={18} />} tone={withGrant === 0 ? "warn" : "good"} />
        <StatCard label="Devices" value={withDevice} detail="active Android devices" icon={<Smartphone size={18} />} tone={withDevice === 0 ? "warn" : "neutral"} />
        <StatCard label="Source Review" value={candidates.length} detail="candidate mappings" icon={<UserCog size={18} />} tone={candidates.length > 0 ? "warn" : "neutral"} />
      </div>

      <div className="mt-6 grid gap-5 xl:grid-cols-[minmax(0,1.2fr)_minmax(420px,0.8fr)]">
        <Panel title="Roster">
          {operators.length === 0 ? (
            <EmptyPanel message="No operators returned." />
          ) : (
            <div className="overflow-x-auto">
              <table className="min-w-full text-left text-sm">
                <thead className="text-xs uppercase text-[#8899AA]">
                  <tr>
                    <th className="px-3 py-2 font-semibold">Operator</th>
                    <th className="px-3 py-2 font-semibold">Status</th>
                    <th className="px-3 py-2 font-semibold">Scope</th>
                    <th className="px-3 py-2 text-right font-semibold">Gates</th>
                    <th className="px-3 py-2 font-semibold">Updated</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#334155] text-[#E0E8F0]">
                  {operators.map((operator) => (
                    <tr key={operator.operator_id} className={selectedOperator?.operator_id === operator.operator_id ? "bg-[rgba(20,241,217,0.06)]" : undefined}>
                      <td className="px-3 py-3">
                        <a href={`/operators?operator_id=${encodeURIComponent(operator.operator_id)}`} className="font-semibold text-white hover:text-[#14F1D9]">
                          {operator.display_name}
                        </a>
                        <div className="mt-1 text-xs text-[#8899AA]">{joinParts([operator.display_code, shortId(operator.operator_id), formatLabel(operator.primary_role_hint)])}</div>
                      </td>
                      <td className="px-3 py-3">
                        <StatusBadge status={operator.status} />
                      </td>
                      <td className="px-3 py-3 text-[#B0BEC5]">{dash(operator.primary_location)}</td>
                      <td className="px-3 py-3 text-right text-xs text-[#93a4b8]">
                        {operator.grant_count} grants · {operator.capability_count} caps · {operator.active_device_count} devices
                      </td>
                      <td className="px-3 py-3 text-[#8899AA]">{dateTime(operator.updated_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>

        <Panel title="Profile Detail">
          {selectedError ? (
            <ErrorPanel error={selectedError} />
          ) : selectedOperator ? (
            <ProfileDetail operator={selectedOperator} grants={grants} grantsError={grantsError} devices={devices} devicesError={devicesError} returnTo={returnTo} />
          ) : (
            <EmptyPanel message="Select an operator." />
          )}
        </Panel>
      </div>

      <div className="mt-6">
        <Panel title="Source Candidates" description={candidates.length > 0 ? `${candidates.length.toLocaleString("en-IN")} open mappings` : undefined}>
          {candidatesError ? (
            <ErrorPanel error={candidatesError} />
          ) : candidates.length === 0 ? (
            <EmptyPanel message="No source candidates returned." />
          ) : (
            <div className="grid gap-3 lg:grid-cols-2">
              {candidates.map((candidate) => (
                <SourceCandidateCard key={candidate.candidate_id} candidate={candidate} operators={operators} returnTo={returnTo} />
              ))}
            </div>
          )}
        </Panel>
      </div>
    </>
  );
}

function ProfileDetail({
  operator,
  grants,
  grantsError,
  devices,
  devicesError,
  returnTo,
}: {
  operator: OperatorRow;
  grants: GrantRow[];
  grantsError: Parameters<typeof ErrorPanel>[0]["error"] | null;
  devices: DeviceRow[];
  devicesError: Parameters<typeof ErrorPanel>[0]["error"] | null;
  returnTo: string;
}) {
  return (
    <div className="space-y-5">
      <div>
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 className="text-lg font-bold text-white">{operator.display_name}</h2>
            <p className="mt-1 text-sm text-[#93a4b8]">{joinParts([operator.display_code, shortId(operator.operator_id), formatLabel(operator.primary_role_hint)])}</p>
          </div>
          <StatusBadge status={operator.status} />
        </div>
        <div className="mt-4 grid gap-2 sm:grid-cols-3">
          <StatPill label="Grants" value={operator.grant_count} tone={operator.grant_count > 0 ? "good" : "warn"} />
          <StatPill label="Capabilities" value={operator.capability_count} tone={operator.capability_count > 0 ? "good" : "warn"} />
          <StatPill label="Devices" value={operator.active_device_count} tone={operator.active_device_count > 0 ? "good" : "warn"} />
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        {operator.status !== "active" ? (
          <form action={activateOperatorAction} className="rounded-lg border border-[#1f8f65] bg-[#102018] p-3">
            <input type="hidden" name="operator_id" value={operator.operator_id} />
            <input type="hidden" name="row_version" value={operator.row_version} />
            <input type="hidden" name="return_to" value={returnTo} />
            <input type="hidden" name="reason" value="Activated from Operator Management." />
            <ConfirmSubmitButton message={`Activate ${operator.display_name}?`} className="inline-flex h-9 items-center justify-center rounded-md border border-[#22c55e]/60 px-3 text-sm font-bold text-[#86efac] hover:border-[#22c55e]">
              Activate
            </ConfirmSubmitButton>
          </form>
        ) : null}
        {operator.status === "active" ? (
          <form action={deactivateOperatorAction} className="rounded-lg border border-[#7f1d1d] bg-[#1d1214] p-3">
            <input type="hidden" name="operator_id" value={operator.operator_id} />
            <input type="hidden" name="row_version" value={operator.row_version} />
            <input type="hidden" name="return_to" value={returnTo} />
            <input type="hidden" name="reason" value="Deactivated from Operator Management." />
            <ConfirmSubmitButton message={`Deactivate ${operator.display_name}?`} className="inline-flex h-9 items-center justify-center rounded-md border border-[#ef4444]/60 px-3 text-sm font-bold text-[#fca5a5] hover:border-[#ef4444]">
              Deactivate
            </ConfirmSubmitButton>
          </form>
        ) : null}
      </div>

      <section>
        <h3 className="text-sm font-bold text-white">Grants</h3>
        {grantsError ? (
          <div className="mt-3"><ErrorPanel error={grantsError} /></div>
        ) : grants.length === 0 ? (
          <div className="mt-3"><EmptyPanel message="No grants returned." /></div>
        ) : (
          <div className="mt-3 space-y-2">
            {grants.map((grant) => (
              <div key={grant.grant_id} className="rounded-md border border-[#334155] bg-[#10141b] p-3 text-sm">
                <div className="flex items-start justify-between gap-3">
                  <span className="font-semibold text-white">{formatLabel(grant.role)}</span>
                  <StatusBadge status={grant.status} />
                </div>
                <div className="mt-2 text-xs text-[#93a4b8]">{joinParts([grant.scope_type, shortId(grant.scope_id), dateTime(grant.valid_from)])}</div>
              </div>
            ))}
          </div>
        )}
      </section>

      <section>
        <h3 className="text-sm font-bold text-white">Devices</h3>
        {devicesError ? (
          <div className="mt-3"><ErrorPanel error={devicesError} /></div>
        ) : devices.length === 0 ? (
          <div className="mt-3"><EmptyPanel message="No devices returned." /></div>
        ) : (
          <div className="mt-3 space-y-2">
            {devices.map((device) => (
              <div key={device.device_id} className="rounded-md border border-[#334155] bg-[#10141b] p-3 text-sm">
                <div className="flex items-start justify-between gap-3">
                  <span className="font-semibold text-white">{device.app_install_id}</span>
                  <StatusBadge status={device.status} />
                </div>
                <div className="mt-2 text-xs text-[#93a4b8]">{joinParts([device.app_version, device.os_version, `seen ${dateTime(device.last_seen_at)}`])}</div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function SourceCandidateCard({ candidate, operators, returnTo }: { candidate: CandidateRow; operators: OperatorRow[]; returnTo: string }) {
  const activeOperators = operators.filter((operator) => operator.status === "active");
  return (
    <div className="rounded-lg border border-[#334155] bg-[#10141b] p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="font-semibold text-white">{formatLabel(candidate.source_flow)}</div>
          <div className="mt-1 break-all font-mono text-xs text-[#93a4b8]">{candidate.external_ref_hash}</div>
        </div>
        <span className="shrink-0 rounded border border-[#334155] px-2 py-1 text-[10px] uppercase text-[#93a4b8]">{candidate.source_system}</span>
      </div>
      <div className="mt-3 grid gap-2 text-xs text-[#93a4b8] sm:grid-cols-2">
        <span>{formatLabel(candidate.external_ref_type)}</span>
        <span>{Math.round(candidate.confidence * 100)}% confidence</span>
        <span>{candidate.observation_count.toLocaleString("en-IN")} observations</span>
        <span>{dateTime(candidate.last_seen_at)}</span>
        <span>{dash(candidate.observed_role_hint)}</span>
        <span>{dash(candidate.observed_scope_hint)}</span>
      </div>
      <div className="mt-4 grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto]">
        <form action={mapSourceCandidateAction} className="flex min-w-0 flex-wrap items-end gap-2">
          <input type="hidden" name="candidate_id" value={candidate.candidate_id} />
          <input type="hidden" name="row_version" value={candidate.row_version} />
          <input type="hidden" name="return_to" value={returnTo} />
          <label className="min-w-[180px] flex-1">
            <span className="text-xs uppercase text-[#93a4b8]">Map To</span>
            <select name="operator_id" className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-2 text-sm text-white outline-none focus:border-[#14f1d9]">
              {activeOperators.map((operator) => (
                <option key={operator.operator_id} value={operator.operator_id}>
                  {operator.display_name}
                </option>
              ))}
            </select>
          </label>
          <button type="submit" className="h-9 rounded-md border border-[#14f1d9]/50 px-3 text-sm font-bold text-[#14f1d9] hover:border-[#14f1d9]">
            Map
          </button>
        </form>
        <form action={rejectSourceCandidateAction} className="flex items-end">
          <input type="hidden" name="candidate_id" value={candidate.candidate_id} />
          <input type="hidden" name="row_version" value={candidate.row_version} />
          <input type="hidden" name="return_to" value={returnTo} />
          <ConfirmSubmitButton message="Reject this source candidate?" className="h-9 rounded-md border border-[#ef4444]/50 px-3 text-sm font-bold text-[#fca5a5] hover:border-[#ef4444]">
            Reject
          </ConfirmSubmitButton>
        </form>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const active = status === "active";
  const warn = status === "candidate" || status === "suspended";
  const className = active
    ? "border-[#1f8f65] text-[#86efac]"
    : warn
      ? "border-[#a16207] text-[#facc15]"
      : "border-[#7f1d1d] text-[#fca5a5]";
  return (
    <span className={`inline-flex items-center gap-1 rounded border px-2 py-1 text-[10px] font-semibold uppercase ${className}`}>
      {active ? <CheckCircle2 className="h-3 w-3" aria-hidden="true" /> : <AlertTriangle className="h-3 w-3" aria-hidden="true" />}
      {formatLabel(status)}
    </span>
  );
}

function StatCard({
  label,
  value,
  detail,
  icon,
  tone = "neutral",
}: {
  label: string;
  value: number;
  detail: string;
  icon: React.ReactNode;
  tone?: "neutral" | "good" | "warn";
}) {
  const toneClass = tone === "good" ? "text-[#4ade80]" : tone === "warn" ? "text-[#fb923c]" : "text-[#14F1D9]";
  return (
    <div className="rounded-xl border border-[#334155] bg-[#1A1D24] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className={`text-[10px] font-medium uppercase tracking-wider ${toneClass}`}>{label}</p>
          <p className="mt-1.5 text-2xl font-bold text-white">{value.toLocaleString("en-IN")}</p>
          <p className="mt-1 text-xs text-[#8899AA]">{detail}</p>
        </div>
        <div className={`flex h-9 w-9 items-center justify-center rounded-lg bg-[#22262E] ${toneClass}`}>{icon}</div>
      </div>
    </div>
  );
}

function returnPath(searchParams: RouteSearchParams): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams)) {
    if (typeof value === "string") params.set(key, value);
  }
  const qs = params.toString();
  return qs ? `/operators?${qs}` : "/operators";
}
