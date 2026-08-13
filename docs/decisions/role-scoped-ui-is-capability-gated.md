# Role-Scoped UI Is Capability-Gated

Status: mandatory guardrail
Owner: Goat OS admin-web surfaces, adminui page-contract compiler, permissions
Applies to: admin-web, backend/internal/adminui (page-contract compiler), backend/internal/permissions, CI, agent skills

## Incident

STG, 2026-08-12. The Verify page (`/verify`, admin-web) gained a filter set for the CEO's
oversight view: module chips (All modules/Counts/Feed/Health/Milk/Vaccination), a capture-date
range picker, a shed filter, and status chips (To verify/Accepted/Rejected). The filters rendered
for EVERY role that can open `/verify`, including `RoleVerifier` — because admin-web pages are
role-agnostic single components: the SAME `VerificationReviewPage` component serves `/verify` and
`/verify?scope_mode=company`, and nothing in the component distinguished who was asking. A
verifier's working queue is one module, one business day, oldest-first; she got a CEO's
cross-tenant filter chrome instead.

## Rule

**Pages are role-agnostic. Role differences come ONLY from two places:**

1. **Permission-gated endpoints** — the backend route or query parameter is authorized (or
   ignored, or 403'd) based on the caller's grants, checked against a named permission constant
   (`backend/internal/permissions/permissions.go`). Never inferred from a role string, never
   inferred from grant *shape* (e.g. "this caller's categories list happens to be empty").
2. **Capability-driven page contracts** — the backend adminui compiler
   (`backend/internal/adminui/app/compiler.go`) decides which controls and option_groups a page
   contract carries, based on the SAME named permission constants. The component renders
   conditionally on the CONTRACT (`controlEnabled(pageContract, "control_id", false)`,
   `optionGroup(pageContract, "group_id")`), never on a role prop, a permission string, or a
   client-side re-implementation of the authorization decision.

**Never:**

- A role-string conditional in a component (`role === "verifier"`, `role.includes("ceo_internal")`).
- A permission-string conditional in a component (`permission === "verification.oversee"`) — the
  component does not see raw permissions at all; it sees the CONTRACT that was already compiled
  against them.
- Per-role page copies (`VerifyPageForCEO.tsx` next to `VerifyPageForVerifier.tsx`) — this
  duplicates the whole page for a chrome difference and rots the moment one copy changes without
  the other; see the copy-firewall direction this repo already takes on data literals.
- Inferring "this caller is leadership" from the shape of an unrelated field the backend already
  returns (e.g. an authorized-categories list happening to come back empty). That shape can change
  for other reasons; a real capability check does not.

## Applied To The Verify Page

The fix added `permissions.VerificationOversee` (`verification.oversee`,
`backend/internal/permissions/permissions.go`), granted to `RoleCEOInternal` and `RolePCDirector`
— the two roles that already receive the unrestricted, cross-category branch of
`resolveVerifierCategories` in `backend/internal/verification/adapters/http/handler.go`
(`VerificationReview` without `VerificationVerdict`). The new capability makes that existing
distinction an explicit, checkable permission instead of an inference over grant shape.

Both gate layers were wired:

- **Contract (pixels):** `compileVerificationReviewControls`
  (`backend/internal/adminui/app/compiler.go`) adds an `oversight_filters` control, enabled only
  when the caller holds `VerificationOversee`. `verification-review-page.tsx` reads it once
  (`const oversightFiltersEnabled = controlEnabled(pageContract, "oversight_filters", false)`) and
  wraps the module-chip row and the capture-date range picker (`<ActionsDateFilter>`) in it.
- **Data (the gate that actually matters):** `ports.ListQueueParams.OversightFiltersEnabled`, set
  by the handler from the caller's grants. When false, the app layer
  (`backend/internal/verification/app/service.go`) ignores `nav_module` and the
  `business_date_from`/`business_date_to` range — falling back to the caller's normal one-day
  queue rather than 403'ing a stale bookmark — and leaves `filter_options.modules` empty, so the
  module-chip row (already conditional on `modules.length > 1`) has nothing to render even if a
  contract check were somehow bypassed.

**"The gate guards data, not pixels."** A UI-only gate is a suggestion; a caller who edits the URL
or replays a captured request bypasses it entirely. Both layers must independently enforce the
same rule.

**What stayed ungated:** the status chips (To verify/Accepted/Rejected) and the shed filter. Git
history showed both predate the oversight rollout — the shed filter and its Apply/Clear pair were
in the page's original commit (`89b16c0fa`, the verifier's first working-queue screen), and the
status chips predate that (`fe06be1ed`, the page's earliest commit). They are the verifier's own
working-queue filters, not oversight chrome, and removing them would have regressed her queue.

## How To Ask For Role-Scoped UI In An Agent Prompt

State the SPLIT explicitly, not just the visible difference:

- Bad: "Hide the export button from operators."
- Good: "Add a permission `X.export` (name it per the conventions in
  `backend/internal/permissions/permissions.go` — which roles get it and why, in a doc comment).
  Gate the export control in the page's compiler function
  (`backend/internal/adminui/app/compiler.go`) on that permission. The component must read
  `controlEnabled(pageContract, "export", false)`, never a role prop. Also gate the export
  ENDPOINT itself on the same permission — the control hides the button, the endpoint is what
  actually stops the write."

Always ask for BOTH halves (contract control + endpoint enforcement) in one request. A prompt that
asks only for the button to disappear produces exactly the STG incident's inverse: a pixel-only
gate with no data enforcement behind it.

## CI And Review

```bash
make role-scoped-ui-contract-guard
```

Guard source: `tools/agent-hooks/check-role-scoped-ui-contract.mjs`. It is narrow and textual
(same shape as `check-operational-partition-identity.mjs` and
`check-leadership-verifier-surface-separation.mjs`): it scans a fixed, extendable list of
admin-web page files (`TARGET_FILES` in the guard) for (1) role/permission string literals driving
a rendering branch, and (2) known oversight-only chrome markers present without a
`controlEnabled(pageContract, ...)` gate anywhere in the file. Add a page's file path to
`TARGET_FILES` when it grows a role-differentiated filter/control set that must stay
contract-driven.

Review checklist:

- Does every role-differentiated control read a named permission constant, never a role/permission
  string compared in a component?
- Is there a page-contract control (or option_group) backing every role-conditional render?
- Does the SAME permission gate the backend endpoint/query params, not just the contract control?
- Would a hand-edited URL or a replayed request reach the restricted data without holding the
  permission?
- Do pre-existing, role-agnostic filters (working-queue chrome that predates the split) stay
  ungated for the role that always had them?

## Related Decisions

- `docs/decisions/leadership-vs-verifier-surface-separation.md` — the separate but related rule
  that Leadership and Verifier must be on SEPARATE ROUTES with no shared UI layer at all. This
  decision covers the case where ONE page legitimately serves multiple roles and must vary its
  chrome by capability rather than by route.
- `backend/internal/permissions/permissions.go` — `VerificationOversee` doc comment; the source of
  truth for every permission constant and which roles hold it.
- `backend/internal/adminui/app/compiler.go` — `compileVerificationReviewControls`; the pattern to
  copy for any new capability-gated control.
