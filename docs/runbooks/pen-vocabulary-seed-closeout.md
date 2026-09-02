# Pen Vocabulary Seed Closeout

When applying `000240_sop_library_pen_vocabulary.sql` to an environment with existing seed data,
run the normal migration path and then verify the admin-web bootstrap and affected SOP records use
the product word `pen` for reader-facing copy.

This is a copy-only closeout. Do not reseed or rewrite source identifiers: `shed_id`, `shed_tag`,
route paths, SOP codes, field keys, and stored enum values remain the data contract.

Minimum post-migration checks:

- `/admin-web/bootstrap` has no user-visible `shed` copy outside named dead-copy exceptions.
- Published SOP descriptions and `form_dsl` labels for vaccination drive, feed direction,
  feed transport, feed packing, and weighing session read with `pen` or `physical location` as
  appropriate.
- Existing `verification_items.subject_label` prefixes have moved from `Whole shed` and `Shed move`
  to `Whole pen` and `Pen move`.
