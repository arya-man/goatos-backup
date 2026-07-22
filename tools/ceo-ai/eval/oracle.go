package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// OracleRunner resolves the independent ground truth for a question by running
// its SQL through `psql`. Using the libpq CLI keeps this module stdlib-only and
// avoids vendoring a Postgres driver. The runner is read-only by construction:
// validateOracle already rejects any non-SELECT token, and psql runs the single
// statement the golden file declares.
type OracleRunner struct {
	DSN      string
	TenantID string
	Timeout  time.Duration
}

// resolve runs the oracle SQL and returns a resolved OracleResult. It never
// panics; a query failure is captured in OracleResult.Err so one bad oracle
// fails only its own question, not the whole run.
func (r OracleRunner) resolve(ctx context.Context, q GoldenQuestion) OracleResult {
	if q.Oracle == nil {
		return OracleResult{Applicable: false}
	}
	out, err := r.runPSQL(ctx, q.Oracle.SQL)
	if err != nil {
		return OracleResult{Applicable: true, Err: err.Error()}
	}
	switch q.Oracle.Kind {
	case oracleScalar:
		v, perr := parseScalar(out)
		if perr != nil {
			return OracleResult{Applicable: true, Err: perr.Error()}
		}
		return OracleResult{Applicable: true, Scalar: v}
	case oracleRows:
		rows, perr := parseRows(out)
		if perr != nil {
			return OracleResult{Applicable: true, Err: perr.Error()}
		}
		return OracleResult{Applicable: true, Rows: rows}
	default:
		return OracleResult{Applicable: true, Err: "unknown oracle kind " + q.Oracle.Kind}
	}
}

// runPSQL executes one statement with tuples-only, unaligned output and the
// tenant bound as the psql variable :'tenant_id'. The SQL is fed on stdin (not
// -c) because psql only performs :'var' interpolation for input read from a file
// or stdin. ON_ERROR_STOP makes psql exit non-zero on any SQL error so failures
// are never swallowed.
func (r OracleRunner) runPSQL(ctx context.Context, sql string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	args := []string{
		r.DSN,
		"-v", "ON_ERROR_STOP=1",
		"-v", "tenant_id=" + r.TenantID,
		"-A", "-t", "-F", "|",
		"-f", "-",
	}
	cmd := exec.CommandContext(cctx, "psql", args...)
	cmd.Stdin = strings.NewReader(sql)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("psql: %s", msg)
	}
	return stdout.String(), nil
}

func parseScalar(out string) (int64, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("oracle returned no rows for scalar_int")
	}
	v, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("oracle scalar %q not an integer: %w", fields[0], err)
	}
	return v, nil
}

func parseRows(out string) (map[string]int64, error) {
	rows := map[string]int64{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("oracle rows line %q is not label|value", line)
		}
		label := strings.ToLower(strings.TrimSpace(parts[0]))
		v, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("oracle rows value %q not an integer: %w", parts[1], err)
		}
		rows[label] = v
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("oracle returned no rows for kind=rows")
	}
	return rows, nil
}
