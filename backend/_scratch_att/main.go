// Throwaway E2E runner (NOT committed): runs the production hrms-attendance stage once.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/vgoats/goatos/backend/internal/kernelstages"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	ctx := context.Background()
	cfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		fmt.Println("connect:", err)
		os.Exit(1)
	}
	defer pool.Close()
	stage := kernelstages.NewHRMSAttendanceStage(kernelstages.Deps{Pool: pool, PgCfg: cfg, Logger: observability.New(observability.Config{Service: "scratch"})}, os.Getenv("GOATOS_TENANT_ID"))
	fmt.Println("stage:", stage.Name(), "err:", stage.Run(ctx))
}
