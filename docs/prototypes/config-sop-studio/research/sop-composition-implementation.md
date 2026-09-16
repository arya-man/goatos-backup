# Local SOP composition implementation

Files: `sop-composition.js`, `sop-composition.css`, `judge-sop-composition.cjs`. Load after existing editor/compiler/runner wrappers. No standalone event/rule directory is part of this implementation.

## Contract for examples

Existing active and archived SOPs retain a generated stable `workflowId`; `compositionWorkflows()` returns `{id,module,workflow}`. A graph action uses `action: 'Follow another SOP'` and `childWorkflowId`. A master SOP may additionally hold:

```js
stagePlan: { stages: [{
  id: 'transit', label: 'Transit', childWorkflowId: 'stable-child-id',
  dependencies: [{stageId: 'purchase', state: 'approved'}],
  approvalRequired: true, approvalRole: 'Director',
  waitDays: 0, repeatCheckHours: 3
}] }
```

Dependency states are started, completed and approved. Dependency cycles, unknown stages/children, nested self/ancestor references and invalid times are rejected. Approval dependencies must point to stages that require approval. Stage controls appear under **Stages and approvals** within the existing SOP editor; graph actions also expose the child selector.

## Publication and execution

Compilation copies the child's published definition if one exists, including its version; otherwise it compiles the current child draft and labels it as a draft copy. Parent snapshots store child definitions. Later edits do not change a running or published parent. Nested child execution enters the real existing operator runner, isolates child answers and returns to the parent only at the child End. This is authored-flow completion, not independent proof that a real animal/task has completed.

Stage practice saves a frozen master definition under `state.compositionPractices[masterWorkflowId]`. A stage starts only when dependencies allow it. Started dependencies permit parallel destination preparation. Stage completion requires finished child execution, configured wait time and any required approval. Approval checks the exact selected local role; operator acknowledgement is not approval. Practice time advances explicitly. Repeated checks require local photo/video file metadata in their scheduled intervals; missed intervals block completion and require restarting the practice. Files are not uploaded or cryptographically verified.

## Tests and limits

The composition judge loads the actual application stack, then the composition wrapper if not yet included. It checks compiler missing/cycle guards, immutable child contents/revision, started-state parallelism, complete-state ordering, exact approval role, elapsed waiting time, periodic evidence, actual nested runner pause/return and answer isolation. It does not certify browser layout or production integration.

No generic production scheduler, backend mutation, real approval, vaccination protocol, transport service or feed experiment engine is claimed. Parent-owned business examples and staging/reference imports must preserve their source meanings and existing local drafts. Legacy standalone orchestration code is outside this new surface and should remain unloaded.

## Integrated timing and nested-master corrections

Stages may bind `waitDaysRef` and `repeatCheckHoursRef` to a configuration key or `config:<stable item ID>`. Compilation requires an active department-accessible numeric setting with days/hours units, substitutes the value, and pins its ID/key/revision/value/unit under `settingSnapshots`. Published/running plans remain unchanged by later setting edits, archive or unlink; new compilations fail closed. Timing references participate in item usage/impact review. The procurement example now references the shared holding/travel/warm-up/check settings rather than copying numeric periods.

A child SOP with its own stage plan now enters a nested stage practice instead of skipping to the child's graph End. The caller remains paused until all nested stages complete. Nested stage practice carries elapsed practice time back to the containing stage practice. Re-executing child instructions invalidates prior completion evidence and approval; changed child results require approval again. Main operator testing of a master starts stage practice rather than its placeholder graph.

The editor shows the master stages prominently. The plan opens with dependencies and practice controls; editing panels are secondary. Non-approval stages say no approval required, and blocked actions show why. Optional `compositionBusinessGuard(action, run, stageId)` allows the parent-owned procurement example to add local candidate/feed constraints without changing generic stage semantics. The compiler retains `exampleKey` for that explicit example identity.

`run-checks.sh` excludes the retired, unloaded standalone orchestration UI test; composition remains included. The integrated checks passed after these corrections. Independent judges and browser evidence remain separate certification.
