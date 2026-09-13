# Animal purchases: the buying desk films, the CEO decides

Maintainer decision, 2026-09-13.

## The rule

1. **The procurement director records a purchase LOAD on the phone**, in the Procurement
   module's new **Animal purchases** tab beside Vendors and Feed Purchases. A load names the
   vendor (from the register), the farm it is for (CBE / CPT), a load number, roughly how many
   animals, and a note.
2. **Animals are recorded one at a time inside the load.** Goat or sheep, male or female,
   breed, rough age and weight, how the animal looks (healthy / minor concern / unwell), an
   optional temporary tag, a note, and a **video from the in-app camera**. The video is
   mandatory: the write is refused unless the proof is a finished in-app-camera video upload,
   so no candidate row ever exists without a video behind it.
3. **The CEO/CXO watches each video on admin-web and ACCEPTS or REJECTS the animal**, on the
   new `/procurement/animal-purchases` page under Procurement. Accept means "we buy it";
   reject means "we skip it". The decision is version-fenced, recorded once with who decided,
   when, and any note, and cannot be changed afterwards from this screen.
4. **The decision is CEO/CXO ALONE.** `procurement.animal_purchase.decide` is granted only to
   `ceo_internal`, the toxin-verdict shape: the person who films the animal must not be the
   one who accepts it. The procurement desk holds read and write (`.read`, `.write`) and
   never sees the web page.
5. **The phone shows the answer at once.** The load screen re-reads from the server on open
   and on resume, refreshes while it stays open, and the person who recorded the animal gets
   a push (`animal_purchase_decided`) the moment the decision commits. The phone's stored copy
   is a placeholder overwritten by the server read, never treated as truth.
6. **This stage STOPS at the decision.** An accepted animal is NOT written to `goats`, gets
   no RFID, and joins no `procurement_load`. The candidate register is deliberately separate
   from `procurement_loads` / `procurement_load_goats`, whose add-goat write creates a goats
   row on insert -- exactly the write the maintainer said not to make yet. Promotion of
   accepted animals (temporary RFID, joining the real load and the herd) is a later stage.

## The shape

- Tables `animal_purchase_loads` and `animal_purchase_candidates` (migration `000299`).
  Loads are unique on `(tenant_id, load_ref)`; candidates carry a per-load `seq_no` assigned
  under the load's row lock, the video proof reference, and the decision columns with a CHECK
  that `decision = 'pending'` exactly when `decided_at IS NULL`.
- Module `backend/internal/animalpurchase` (domain / ports / app / adapters), the toxin
  module's shape. Every write reserves an idempotency key, audits, and commits in one
  transaction. The decision also emits `procurement.animal_purchase.decided` on the outbox in
  the same transaction (validator branch in migration `000300`), consumed by
  `notificationbridge.AnimalPurchaseNotifyConsumer` which pushes to the recorder (type widened
  in `000301`).
- Permissions ride their own module `animal_purchases` on BOTH surfaces: phone View/Do for
  the procurement director and manager (migration `000302` copies every existing mobile
  `vendors` tick so nobody is re-ticked), web View/Oversee for `ceo_internal`. The CEO floor
  carries the web page for every CXO.
- Routes: `/app/procurement/animal-purchases/*` for the phone (read / write), and
  `/procurement/animal-purchases/review` + `/procurement/animal-purchases/animals/{id}/decision`
  for the CEO. Videos are signed beside the list read (one proof read per page), never
  resolved one by one from the web tier.
- Counts on a load (total / awaiting / accepted / rejected) are WHOLE-LOAD aggregates computed
  in the read, never page sums; the review page's chip counts are whole-filter.

## What is deliberately not here

- No RFID, no goat, no herd write, no procurement_load link (rule 6).
- No verifier: this is an approval gate in the toxin / counts_approver shape, not a
  Verification category, so `verification.verdict` stays verifier-only.
- No editing or reversing a decision from the screen. A wrong decision is a maintainer
  conversation and a data repair, not a second button.
