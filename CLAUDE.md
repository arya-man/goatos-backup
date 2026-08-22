@AGENTS.md

## Hard Local Resource Rule

For Goat OS on Ravi's laptop, do not start Colima, Docker Desktop,
`goatos-local-current`, or local Docker Postgres when the OCI Postgres tunnel on
`127.0.0.1:15432` is available. Do not run commands like `colima start`,
`docker start goatos-local-current`, or Postgres integration tests with
`GOATOS_RUN_POSTGRES_TESTS=1` unless Ravi explicitly asks for a local Docker DB,
Docker-specific test, or disposable mutation database.
