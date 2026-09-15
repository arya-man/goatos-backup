# Direct canvas UX acceptance

Status: implementation pending. Earlier editor signoffs do not cover the newly requested direct manipulation UX.

## Required interactions

1. Add question/action/decision/end from an obvious palette; select and edit text/type/units/source through properties.
2. Connect using visible input/output ports, both pointer drag and click-source/click-target. Output labels identify Next, condition/answer or Otherwise.
3. Choosing a question then inserting a decision uses that question as its answer source. Condition/range labels appear on the graph, not only in a detached inspector.
4. Insert from an edge plus: preserve original upstream branch and downstream target, select new node for authoring.
5. Select a connection to remove/reconnect it. Delete a simple linear node can reconnect neighbours; branching deletion exposes impacted paths rather than silently choosing one.
6. Escape cancels pending connections. Panning background does not create/select nodes accidentally. Zoom and fit retain usable coordinates and include all graph nodes.
7. Shared questions/actions/items and source availability remain functional; edits persist; published snapshots remain frozen. Validation still blocks missing paths/cycles/unavailable resources.
8. Mobile ports and properties remain reachable, with click-to-connect alternative to precise dragging.

## Actual Chrome proof required

Create a workflow, add a question, connect it, insert a step on an existing edge, delete and repair a step, then validate and simulate the resulting route. Verify labels match the question/branch and no destination dropdown was necessary. Test fit/zoom after moving a node. Capture desktop and narrow editor states after final edits.

## Reference comparison

Claude flow.js exposes edge-local + insertion and clickable validation findings. These patterns reduce detached authoring, but borrowing their appearance alone does not satisfy direct port connection. Existing app.js destination dropdowns may remain only as secondary accessibility controls if the canvas itself owns the normal workflow.

## Final desktop scoped receipt

Status: PASS for desktop direct authoring in the tested local Counts workflow, 15 September 2026. Mobile usability is not signed off by this receipt.

Independent native Chrome test on Counts/Editor, “Daily count • canvas review”:
- Edge insertion controls are actual HTML buttons in native accessibility tree, naming source, branch and destination.
- Clicked Otherwise output then approval input: edge label changed to Otherwise before Request supervisor approval.
- Undo restored Otherwise before Recount the pen.
- Clicked edge plus after Recount, inserted Action: graph gained Recount → New action → Finish.
- Deleted inserted Action: graph restored Recount → Finish automatically.
- Fit displayed all six nodes; Validate returned Ready to test.
- Operator No branch reached Recount, acknowledgement reached Finish.
- Operator Yes branch reached Request supervisor approval, acknowledgement reached Finish.
- Final desktop screenshot visually checked ports, branch labels, whole connected graph and properties panel; destination selectors were absent.

Root's separate actual CUA pointer tests: Recount node DOM left325px → 618.245px after drag; Otherwise output drag to approval input changed the accessible edge label; Undo restored the recount destination. My native coordinate drag attempt did not visibly move the node, so it is not counted as proof; the successful browser pointer evidence belongs to root.

Remaining scope limits: mobile editor is bounded according to root's390px check but has a tall toolbar and requires scrolling; no mobile usability signoff. Clinical engine, production operator runtime and deployment are outside this local canvas receipt. Underlying source/functional regression tests are recorded separately by the functional judge.
