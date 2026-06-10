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
	log := New(Config{Service: "svc", Sink: "otlp", W: &buf})
	log.Info("otlp_sink")
	if !strings.Contains(buf.String(), `"msg":"otlp_sink"`) {
		t.Errorf("expected structured JSON output, got: %s", buf.String())
	}
}

func TestNewSinkGCMFallsBackToJSON(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Service: "svc", Sink: "gcm", W: &buf})
	log.Info("gcm_sink")
	if !strings.Contains(buf.String(), `"msg":"gcm_sink"`) {
		t.Errorf("expected structured JSON output, got: %s", buf.String())
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
