package taskqueue

import "testing"

func TestConfigFromEnvDisabledWhenUnset(t *testing.T) {
	for _, key := range []string{
		"GOATOS_CLOUD_TASKS_PROJECT_ID",
		"GOOGLE_CLOUD_PROJECT",
		"GOATOS_CLOUD_TASKS_LOCATION",
		"GOATOS_REGION",
		"GOATOS_CLOUD_TASKS_QUEUE_ID",
		"GOATOS_NEAR_TERM_TASK_QUEUE_ID",
		"GOATOS_NOTIFICATION_DISPATCHER_RUN_URL",
		"GOATOS_CLOUD_TASKS_OAUTH_SERVICE_ACCOUNT",
	} {
		t.Setenv(key, "")
	}
	_, enabled, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if enabled {
		t.Fatal("expected disabled config")
	}
}

func TestConfigFromEnvRequiresCompleteConfig(t *testing.T) {
	t.Setenv("GOATOS_CLOUD_TASKS_QUEUE_ID", "goatos-dev-near-term-kernel")
	if _, enabled, err := ConfigFromEnv(); err == nil || enabled {
		t.Fatalf("expected incomplete config error, enabled=%v err=%v", enabled, err)
	}
}

func TestConfigFromEnvIgnoresGenericProjectWhenTaskConfigUnset(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "goatos-dev")
	_, enabled, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if enabled {
		t.Fatal("generic GOOGLE_CLOUD_PROJECT alone should not enable Cloud Tasks")
	}
}

func TestConfigFromEnvComplete(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "goatos-dev")
	t.Setenv("GOATOS_CLOUD_TASKS_LOCATION", "asia-south1")
	t.Setenv("GOATOS_CLOUD_TASKS_QUEUE_ID", "goatos-dev-near-term-kernel")
	t.Setenv("GOATOS_NOTIFICATION_DISPATCHER_RUN_URL", "https://run.googleapis.com/apis/run.googleapis.com/v1/namespaces/goatos-dev/jobs/goatos-dev-notification-dispatcher:run")
	t.Setenv("GOATOS_CLOUD_TASKS_OAUTH_SERVICE_ACCOUNT", "goatos-scheduler-dev@goatos-dev.iam.gserviceaccount.com")
	cfg, enabled, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if !enabled || cfg.ProjectID != "goatos-dev" || cfg.QueueID == "" || cfg.TargetURL == "" {
		t.Fatalf("unexpected config enabled=%v cfg=%#v", enabled, cfg)
	}
}

func TestSafeTaskID(t *testing.T) {
	got := SafeTaskID("tenant:calendar reminder / 2026-06-27T01:02")
	if got != "tenant-calendar-reminder-2026-06-27T01-02" {
		t.Fatalf("SafeTaskID = %q", got)
	}
}
