-- +goose Up
-- Two feed verification items rendered their location INSIDE the subject label, which the card
-- already shows from its own fields.
--
--   distribution: "Session 2 · 62241795-628e-58ef-9591-aa384fb0f0f7 · Pen 1"
--   transport:    "Feed transport · Yashoda"
--
-- The distribution one is the worse of the two: that middle fragment is a raw shed UUID rendered as
-- farm copy, because the label was composed from shed_ID rather than a resolved name. The transport
-- one printed the shed a second time on every card, beside the category chip that already says Feed
-- Transport.
--
-- The producers no longer compose either (the location rides on shed_id/partition_label and is
-- composed once at the wire boundary by oploc.Display()). This repairs the rows already queued,
-- which would otherwise keep showing a UUID to a verifier until each item is decided and aged out.
--
-- Distribution takes its session from the source completion, the same row the label was always
-- meant to describe -- a 1:1 join on that completion's primary key, so no item can take another
-- session's number. Transport has no session and needs no subject at all.
UPDATE public.verification_items vi
SET subject_label = 'Session ' || c.session_no::text,
    updated_at = now()
FROM public.feed_distribution_completions c
WHERE vi.tenant_id = c.tenant_id
  AND vi.source_ref_type = 'feed_distribution_completion'
  AND vi.source_ref_id = c.completion_id
  AND c.session_no > 0
  AND vi.subject_label IS DISTINCT FROM 'Session ' || c.session_no::text;

UPDATE public.verification_items
SET subject_label = NULL,
    updated_at = now()
WHERE source_ref_type = 'feed_transport_attempt'
  AND subject_label IS NOT NULL;

-- +goose Down
-- Irreversible by design: the old labels embedded a raw shed id and a duplicated pen, and nothing
-- records what each row previously read. Re-running the Up is idempotent, which is the recovery path
-- that matters; restoring a UUID into user-facing copy is not a state worth returning to.
SELECT 1;
