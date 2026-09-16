package kernelstages

import (
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestMain tears down the package-scoped pgtest container once after all tests run. Required for
// every package that calls pgtest.StartPostgres (enforced by TestLifecycleContractGuard).
var stageMetricReader *sdkmetric.ManualReader

func TestMain(m *testing.M) {
	stageMetricReader = sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(stageMetricReader)))
	pgtest.RunMain(m)
}
