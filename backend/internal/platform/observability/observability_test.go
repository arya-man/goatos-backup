package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewProducesStructuredOutput(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{
		Service: "test-svc",
		Version: "v0.1.0",
		Env:     "test",
		W:       &buf,
	})
	log.Info("hello_world", "key", "value")

	out := buf.String()
	if out == "" {
		t.Fatal("expected log output, got empty string")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if m["service"] != "test-svc" {
		t.Errorf("expected service=test-svc, got %v", m["service"])
	}
	if m["version"] != "v0.1.0" {
		t.Errorf("expected version=v0.1.0, got %v", m["version"])
	}
	if m["env"] != "test" {
		t.Errorf("expected env=test, got %v", m["env"])
	}
	if m["msg"] != "hello_world" {
		t.Errorf("expected msg=hello_world, got %v", m["msg"])
	}
}

func TestNewDefaultsToStdoutJSON(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Service: "svc", W: &buf})
	log.Info("check")
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &m); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
}

func TestNewSinkOTLPFallsBackToJSON(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv("GOATOS_OTLP_ENDPOINT", "https://user:password@collector.example.test/v1/logs?token=secret")
	log := New(Config{Service: "svc", Sink: "otlp", W: &buf})
	log.Info("otlp_sink")
	lines := logLines(t, buf.String())
	if len(lines) != 2 {
		t.Fatalf("log lines=%d, want startup warning plus message:\n%s", len(lines), buf.String())
	}
	if lines[0]["msg"] != "observability_logs_stdout_metrics_traces_otlp" {
		t.Fatalf("first log msg=%v, want startup notice", lines[0]["msg"])
	}
	if lines[0]["requested_sink"] != "otlp" || lines[0]["log_destination"] != "stdout_json" {
		t.Fatalf("bad otlp notice fields: %#v", lines[0])
	}
	if lines[0]["otlp_endpoint_configured"] != true || lines[0]["otlp_endpoint_target"] != "https://collector.example.test" {
		t.Fatalf("bad sanitized endpoint fields: %#v", lines[0])
	}
	if strings.Contains(buf.String(), "password") || strings.Contains(buf.String(), "secret") || strings.Contains(buf.String(), "/v1/logs") {
		t.Fatalf("otlp warning leaked raw endpoint detail: %s", buf.String())
	}
	if lines[1]["msg"] != "otlp_sink" {
		t.Errorf("expected structured JSON output, got: %#v", lines[1])
	}
}

func TestNewUnknownSinkWarnsAndFallsBackToJSON(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Service: "svc", Sink: "gmc", W: &buf})
	log.Info("unknown_sink")
	lines := logLines(t, buf.String())
	if len(lines) != 2 {
		t.Fatalf("log lines=%d, want startup warning plus message:\n%s", len(lines), buf.String())
	}
	if lines[0]["msg"] != "observability_sink_unknown" {
		t.Fatalf("first log msg=%v, want unknown sink warning", lines[0]["msg"])
	}
	if lines[0]["requested_sink"] != "gmc" || lines[0]["fallback_sink"] != "stdout_json" {
		t.Fatalf("bad unknown sink warning fields: %#v", lines[0])
	}
	if lines[1]["msg"] != "unknown_sink" {
		t.Errorf("expected structured JSON output, got: %#v", lines[1])
	}
}

func TestNewSinkGCMFallsBackToJSON(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Service: "svc", Sink: "gcm", W: &buf})
	log.Info("gcm_sink")
	lines := logLines(t, buf.String())
	if len(lines) != 2 {
		t.Fatalf("log lines=%d, want startup warning plus message:\n%s", len(lines), buf.String())
	}
	if lines[0]["msg"] != "observability_logs_stdout_metrics_traces_otlp" || lines[0]["requested_sink"] != "gcm" {
		t.Fatalf("bad gcm notice fields: %#v", lines[0])
	}
	if lines[1]["msg"] != "gcm_sink" {
		t.Errorf("expected structured JSON output, got: %#v", lines[1])
	}
}

func TestNewSkipSinkNoticeLogSuppressesNotice(t *testing.T) {
	// Regression guard for the SetupTelemetry-vs-New duplicate boot log: the
	// throwaway logger SetupTelemetry builds internally (telemetry.go) must
	// not re-emit the sink notice that a caller's own observability.New(cfg)
	// call already logs. skipSinkNoticeLog is unexported and set only from
	// within this package (telemetry.go), so this test lives here to reach it.
	var buf bytes.Buffer
	cfg := Config{Service: "svc", Sink: "otlp", W: &buf}
	cfg.skipSinkNoticeLog = true
	log := New(cfg)
	log.Info("suppressed_notice_check")

	lines := logLines(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("log lines=%d, want exactly the caller's message (sink notice suppressed):\n%s", len(lines), buf.String())
	}
	if lines[0]["msg"] != "suppressed_notice_check" {
		t.Fatalf("unexpected first line: %#v", lines[0])
	}
}

func TestNewCoalesceVersionDefault(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Service: "svc", W: &buf})
	log.Info("v")
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &m)
	if m["version"] != "dev" {
		t.Errorf("expected default version=dev, got %v", m["version"])
	}
}

func logLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not valid JSON: %v\nline: %s\nall output:\n%s", err, line, raw)
		}
		out = append(out, m)
	}
	return out
}
