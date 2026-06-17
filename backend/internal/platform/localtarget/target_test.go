package localtarget

import "testing"

func TestValidateLocalDatabaseTargetAllowsLocalDatabase(t *testing.T) {
	if err := ValidateLocalDatabaseTarget("test-command", "local", "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable"); err != nil {
		t.Fatalf("local target rejected: %v", err)
	}
	if err := ValidateLocalDatabaseTarget("test-command", "dev", "postgres://postgres:goatos@127.0.0.1:5432/goatos?sslmode=disable"); err != nil {
		t.Fatalf("loopback target rejected: %v", err)
	}
}

func TestValidateLocalDatabaseTargetRejectsUnsafeTargets(t *testing.T) {
	cases := []struct {
		name string
		env  string
		url  string
	}{
		{name: "missing env", env: "", url: "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable"},
		{name: "production env", env: "production", url: "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable"},
		{name: "stg host", env: "local", url: "postgres://postgres:goatos@goatos-stg.internal:5432/goatos?sslmode=disable"},
		{name: "remote host", env: "local", url: "postgres://postgres:goatos@192.0.2.10:5432/goatos?sslmode=disable"},
		{name: "cloudsql socket", env: "local", url: "user=postgres password=goatos dbname=goatos host=/cloudsql/project:region:instance sslmode=disable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLocalDatabaseTarget("test-command", tc.env, tc.url); err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
}

func TestValidateLocalDatabaseTargetAllowsExplicitDevCloudSQL(t *testing.T) {
	t.Setenv("GOATOS_ALLOW_DEV_CLOUDSQL_TARGET", "true")
	t.Setenv("GOATOS_DEV_CLOUDSQL_CONNECTION_NAME", "goatos-dev:asia-south1:goatos-dev-core-db")
	cases := []struct {
		name string
		url  string
	}{
		{
			name: "socket path",
			url:  "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-dev:asia-south1:goatos-dev-core-db sslmode=disable",
		},
		{
			name: "connection name",
			url:  "user=postgres password=goatos dbname=goatos host=goatos-dev:asia-south1:goatos-dev-core-db sslmode=disable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLocalDatabaseTarget("bq-reconcile --execute", "dev", tc.url); err != nil {
				t.Fatalf("explicit goatos-dev Cloud SQL target rejected: %v", err)
			}
		})
	}
}

func TestValidateLocalDatabaseTargetRejectsCloudSQLWithoutFullDevOptIn(t *testing.T) {
	devURL := "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-dev:asia-south1:goatos-dev-core-db sslmode=disable"
	if err := ValidateLocalDatabaseTarget("bq-reconcile --execute", "dev", devURL); err == nil {
		t.Fatal("goatos-dev Cloud SQL target accepted without opt-in")
	}
	t.Setenv("GOATOS_ALLOW_DEV_CLOUDSQL_TARGET", "true")
	if err := ValidateLocalDatabaseTarget("bq-reconcile --execute", "dev", devURL); err == nil {
		t.Fatal("goatos-dev Cloud SQL target accepted without expected connection name")
	}
	t.Setenv("GOATOS_DEV_CLOUDSQL_CONNECTION_NAME", "goatos-dev:asia-south1:goatos-dev-core-db")

	cases := []struct {
		name string
		env  string
		url  string
	}{
		{
			name: "local env",
			env:  "local",
			url:  devURL,
		},
		{
			name: "spoof substring",
			env:  "dev",
			url:  "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-dev:asia-south1:goatos-dev-core-db-spoof sslmode=disable",
		},
		{
			name: "wrong region",
			env:  "dev",
			url:  "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-dev:us-central1:goatos-dev-core-db sslmode=disable",
		},
		{
			name: "staging",
			env:  "dev",
			url:  "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-stg:asia-south1:goatos-stg-core-db sslmode=disable",
		},
		{
			name: "production",
			env:  "dev",
			url:  "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-prod:asia-south1:goatos-prod-core-db sslmode=disable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLocalDatabaseTarget("bq-reconcile --execute", tc.env, tc.url); err == nil {
				t.Fatal("unsafe Cloud SQL target accepted")
			}
		})
	}
}

func TestValidateLocalDatabaseTargetAllowsExplicitTestEnv(t *testing.T) {
	if err := ValidateLocalDatabaseTarget("seed-dev-grant", "test", "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable", "local", "dev", "test"); err != nil {
		t.Fatalf("test env rejected: %v", err)
	}
}

func TestIsLocalHostAllowsOnlyKnownLocalSocketDirs(t *testing.T) {
	allowed := []string{
		"/tmp/.s.PGSQL.5432",
		"/private/tmp/.s.PGSQL.5432",
		"/run/postgresql/.s.PGSQL.5432",
		"/var/run/postgresql/.s.PGSQL.5432",
	}
	for _, host := range allowed {
		if !IsLocalHost(host) {
			t.Fatalf("local socket host rejected: %s", host)
		}
	}

	rejected := []string{
		"/cloudsql/project:region:instance",
		"/var/run/cloudsql/project:region:instance",
		"/opt/postgres/.s.PGSQL.5432",
	}
	for _, host := range rejected {
		if IsLocalHost(host) {
			t.Fatalf("non-local socket host accepted: %s", host)
		}
	}
}
