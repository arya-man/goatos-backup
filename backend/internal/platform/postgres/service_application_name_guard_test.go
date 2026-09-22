package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// serviceBinaries are the ONLY binaries allowed to tag their pool as Goat OS
// service traffic, which exempts their writes from the manual DB change audit
// (migration 000387, docs/runbooks/manual-db-change-audit.md). They are the
// deployed Cloud Run services / scheduled jobs. Operator CLIs (repairs,
// backfills, seeds, imports, checks) must NOT appear here: their writes are
// exactly what the audit exists to record. Adding a name is a deliberate
// review decision, not a build fix.
var serviceBinaries = []string{
	"analytics-rollup",
	"api",
	"domain-event-consumer",
	"domain-event-processed-sweeper",
	"feed-direction-issue",
	"generate-vaccination-obligations",
	"herd-signals-mqtt-bridge",
	"herd-signals-partition-maintenance",
	"idempotency-key-sweeper",
	"inventory-batch-reconciler",
	"kernel-worker",
	"migrate",
	"notification-dispatcher",
	"obligation-sweeper",
	"outbox-dlq",
	"outbox-relay",
	"partition-maintainer",
	"sop-review-fanout-retry",
}

var serviceNameCall = regexp.MustCompile(`ServiceApplicationName\("([^"]+)"\)`)

func TestOnlyDeployedServicesTagServiceApplicationName(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var tagged []string
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range serviceNameCall.FindAllStringSubmatch(string(data), -1) {
				tagged = append(tagged, m[1])
				if strings.HasPrefix(filepath.ToSlash(path), filepath.ToSlash(filepath.Join(root, "cmd"))+"/") {
					if bin := filepath.Base(filepath.Dir(path)); bin != m[1] {
						t.Errorf("%s tags itself %q; the service name must equal its cmd directory %q", path, m[1], bin)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(tagged)
	if strings.Join(tagged, ",") != strings.Join(serviceBinaries, ",") {
		t.Fatalf("service-tagged binaries drifted from the reviewed allowlist.\n got: %v\nwant: %v\nOperator CLIs must stay untagged so the manual DB change audit records them.", tagged, serviceBinaries)
	}
}
