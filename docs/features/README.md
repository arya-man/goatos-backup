# Feature PRD/TRD Index

This folder holds feature-level PRD/TRD documents for Goat OS work that crosses
phase boundaries or needs its own build-ready contract before implementation.

Phase docs still live in `docs/phases/`. Use this folder when a feature, such
as Mortality, must define its own product semantics, data ownership, scale
model, API contract, parity plan, and rollout gates before code starts.

Current feature docs:

- `docs/features/mortality/PRD.md`
- `docs/features/mortality/TRD.md`

Rules:

- Do not use these docs to bypass the phase roadmap.
- Do not duplicate architecture decisions already owned by `context/` or
  `docs/decisions/`; link to the canonical decision instead.
- Any dashboard/report feature that slices by month, date, breed, farm, load,
  category, status, gender, source, or similar dimensions must follow
  `docs/decisions/high-scale-dashboard-projections.md`.
