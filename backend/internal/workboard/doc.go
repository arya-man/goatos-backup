// Package workboard is the cross-module Work Board read: one row shape that every
// operational module normalises to, one endpoint that the web console and the phone
// both read, and one scope rule that decides which rows a caller sees.
//
// The board owns no table. Each module contributes a Source (ports.Source) over ITS OWN
// rows -- weighing over weighing_work_items, feed over feed_transport_tasks, and so on --
// and emits domain.Row. The board never learns what a bag, a bucket or a dose is; it
// learns a module, a pen, a business date, a work state and an owner.
//
// Isolation is preserved in both directions: a source lives INSIDE its module's package
// and reads only that module's tables plus the org tables every module may read, and
// the board service composes sources through the port without importing any module.
//
// The read is a GLOBAL KEYSET over (module, source_type, source_id), bounded to one
// tenant, one park and one business date per request. Sources are visited in registry
// order; the cursor names where the previous page stopped, so a page touches at most
// the source it stopped in and the ones after it. Sources are never paged
// independently and merged afterwards.
//
// Canonical prose: docs/decisions/work-board.md. Build plan: the Work Board Build Plan
// artifact (10 Sep 2026).
package workboard
