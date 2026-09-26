# Park heads tag animals to a sale, and nothing else of Sales

**Maintainer decision, 2026-09-11.** Status: implemented; rebuilt on main 2026-09-26 (migration
`000443`).

## The decision

The sales desk records a sale on the web (`/sales/config`). The animals physically leave from a
park, so the person standing in the pen with the RFID reader is the **park head**. The park head
therefore gets, on the phone, exactly one thing of Sales: **tag the animals of a sale**. Scan a
tag with the Bluetooth reader or type the number, the animal lands in the basket, its **live weight
(kg)** is typed against it, and the sale is **submitted once every animal is
tagged**. Nothing else is visible: no ledger, no buyer, no money, no pipeline, no vendors.

In the maintainer's words: "they need to have access only to tag animals. They should not see
anything ... They should just see the sale there."

## What changed

**Authority.** `park_head` gains `sales.allocate_animals` and NO other sales permission. On the
web that permission is the tag-animals drawer inside the Sales pages; a park head holds no
`admin_web.bootstrap`, so the web is unaffected. The capability module `sale_allocation` is now
on both surfaces; the mobile row is written for every park head already backfilled by migration
`000443` (the `000245`/`000272` shape, with a ledger so Down removes exactly what Up wrote), and
the same row is added to the `park_head` job's default ticks (`designation_module_defaults`), so
picking "Park head" for a new person on `/people` pre-fills it.

**Scope.** Every allocation route -- the park/pen vocabulary, the picker, the review, the
confirm, the read-back and the queue -- is clamped to the caller's park at the HTTP boundary
(`allowedParkIDs` in `sale_allocation_handler.go`, through
`ResolveAuthorizedParkScopeForCapabilities`, which reads the person's own park ticks). The clamp
covers BOTH halves of a tag: every animal must stand in one of the caller's parks, AND the SALE
must have been recorded at one of them (its `farm` code, read by `salesbridge.ReadSaleDealFarm`).
Clamping only the animals would let a park head tag animals from their own pen onto another
park's sale, or read back any sale's tags, weights and rates by id. A park head asking about
another park is refused `403 park_out_of_scope` before the count gate, so a crafted request
cannot even learn how many animals a sale at another park still needs. The sales desk and the
CXO are tenant-wide, see every park exactly as before, and never pay for the farm lookup. Pinned
by `TestConfirmRefusesAnimalsOutsideTheCallersParkScope`, `TestPreviewAndPickerRefuseAnotherPark`,
`TestPreviewConfirmAndReadBackRefuseASaleAtAnotherPark` (mutation-tested: making the sale clamp
a no-op turns it red) and `TestSaleLocationsNarrowToTheCallersParks`.

**The queue.** `GET /admin/goats/sale-tagging` lists the live animal sales at the caller's
park(s) that still owe animals: date, farm, product, breed, declared, tagged, remaining. **No
buyer and no money on the wire** -- the projection is the access rule. It is served by the same
`salesbridge` that already reads the deal count for the gate, because identity's own SQL stays
out of the sales schema (migration 000177). Park scope is translated to the ledger's farm codes
through the sale-location catalog (a park's `location_code` IS the deal's `farm`), and a scope
that resolves to no farm sees an EMPTY queue rather than every farm's. Pinned by
`TestTaggingQueueNarrowsToTheCallersFarmsAndFailsClosed`.

**Weight per animal, no price** (maintainer decision 2026-09-26, reversing the 2026-09-11
rate). The park head types each animal's live weight -- the existing `animal_weights_kg`,
required on every confirm since 2026-09-08 -- and NO price: the tag-only surface carries no
money anywhere, the queue included. The read-back `GET /admin/goats/sale-allocations/{deal}`
carries one row per animal with its snapshotted tag, pen and weight, which is what the phone shows
a park head resuming a half-tagged sale. A per-animal rate was built on the 2026-09-11 branch and
removed before it shipped; there is no `rate_rupees` column and no `animal_rates_rupees` field.

**The phone module.** Registry key `sale_allocation`, label "Sales", one bar item "Tag animals"
at `/sale-tagging`. Offered on `sales.allocate_animals` **to a principal without `sales.read`**:
the CXO and the sales desk reach the same tag flow inside their Sales module's sale drill, and a
second door onto one flow is what the 2026-09-05 Sales/Procurement split test bans. Pinned by
`TestParkHeadIsOfferedTagOnlySalesAndNothingElseOfSales` (mutation-tested: dropping the
`!SalesRead` half turns the CXO case red).

**Configurable from HRMS** (maintainer instruction, same day; reaffirmed 2026-09-26: park heads
by default, and the `/people` tick for anyone else). The tick on `/people` is the
fact: `sale_allocation` is a capability module on both surfaces, so an admin can tick it
(mobile, do) for ANY person -- an operator at a park, say -- and clear it for a park head. The
phone offer reads the person's HELD permissions (their ticks once migrated), not only the job,
so a tick alone adds the module and clearing it removes the module and the permission together
(person rows decide route authorization). Pinned by `TestTagOnlySalesFollowsThePersonsTicks`
(mutation-tested: reading the role grants instead of the held set turns the operator case red).

## The phone flow

`SaleTaggingListScreen` (L0) lists the queue, Room-first over the cached first page.
`SaleTaggingScreen` (hosted drill) is the basket: the tag field carries the Bluetooth chip that
switches the keyboard-wedge reader on, the same field takes a hand-typed number, both run the
identical lookup (`/admin/goats/sale-candidates` with the park resolved from the sale's farm),
and only an EXACT identifier match lands in the basket -- a substring hit is a different animal
and is offered as a choice instead. Each basket row takes the weight (kg). Submit
is offered only when the sale is filled exactly (`SaleTaggingRules.submitGate`, the client half
of the server's count gate), runs the review first so a refused animal is marked with its
backend reason, then confirms with the weights. If the confirm's reply is lost (a weak signal in the pen)
the screen reads the sale back and, when every basket animal is already on it, shows Done rather
than an error (`SaleTaggingRules.confirmLanded`).

The reader is an app-wide singleton and is released on the first tag, on leaving the screen and
before submit, as the Counts form does.

## What was deliberately not done

- No change to the CXO/sales-desk tag flow (`SaleTagAnimalsScreen`): it keeps its park/pen
  picker and its weights.
- No cross-park tagging: a sale's animals come from the sale's farm; a park head cannot tag a
  CBE sale from CPT, and the sales desk uses the web drawer for anything unusual.
