@AGENTS.md

## Hard Local Resource Rule

For Goat OS on Ravi's laptop, do not start Colima, Docker Desktop,
`goatos-local-current`, or local Docker Postgres when the OCI Postgres tunnel on
`127.0.0.1:15432` is available. Do not run commands like `colima start`,
`docker start goatos-local-current`, or Postgres integration tests with
`GOATOS_RUN_POSTGRES_TESTS=1` unless Ravi explicitly asks for a local Docker DB,
Docker-specific test, or disposable mutation database.

## Vaccination Anchor Dates

Follow the vaccination anchor-date rule in `AGENTS.md`. In short: an anchor date
is baseline vaccine history/start-date semantics for the selected animals, not a
blind one-off drive insert. Use the vaccination kernel/generation path, verify
live/live and live/killed spacing plus same-day caps, and report actual RFID/tag
identifiers rather than internal goat ids. `Z1+Z3` is one vaccine/program label,
not separate `Z1`, `Z2`, and `Z3` stages.

For vaccination drive packing, follow the 200-animals-per-operator-day rule in
`AGENTS.md` and `docs/preventive-care-vaccination/vaccination-rules.md`: pack
complete sheds first, keep sibling partitions under the same parent shed
together when they fit, and do not split a whole shed/group merely to fill the
last seats under 200.
