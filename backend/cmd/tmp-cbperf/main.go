package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

func main() {
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	repo := vaccexecpg.NewRepository(pool, 15*time.Second)
	tenant := "00000000-0000-4000-8000-000000000001"

	run := func(name string, fn func() (any, error)) {
		var best time.Duration = time.Hour
		var payload any
		for i := 0; i < 5; i++ {
			st := time.Now()
			v, err := fn()
			if err != nil {
				fmt.Printf("%-34s ERROR %v\n", name, err)
				return
			}
			if d := time.Since(st); d < best {
				best = d
				payload = v
			}
		}
		b, _ := json.Marshal(payload)
		fmt.Printf("%-34s %8.1f ms   bytes=%d\n", name, float64(best.Microseconds())/1000, len(b))
	}

	run("GET /vaccination/command", func() (any, error) {
		return repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenant})
	})

	// A representative board read, to pick real drilldown cells.
	board, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenant})
	if err != nil {
		panic(err)
	}
	fmt.Printf("  unavailableSections=%v cohortCells=%d shedVaccineCells=%d driveOptions=%d\n",
		board.UnavailableSections, len(board.CohortMatrix), len(board.ShedVaccineMatrix), len(board.DriveOptions))

	run("  closed-without-dose (page 50)", func() (any, error) {
		return repo.CommandBoardClosedWithoutDoseAnimals(ctx, domain.CommandBoardDrilldownQuery{TenantID: tenant, Limit: 50})
	})

	// The worst shed-vaccine cell we can find: most behind animals.
	cells := append([]domain.CommandBoardShedVaccineCell(nil), board.ShedVaccineMatrix...)
	sort.Slice(cells, func(i, j int) bool { return cells[i].BehindAnimals > cells[j].BehindAnimals })
	if len(cells) > 0 && cells[0].BehindAnimals > 0 {
		c := cells[0]
		fmt.Printf("  worst shed-vaccine cell: %s / %s behind=%d\n", c.OperationalLocationDisplay, c.VaccineCode, c.BehindAnimals)
		run("  shed-vaccine animals (page 50)", func() (any, error) {
			return repo.CommandBoardShedVaccineAnimals(ctx, domain.CommandBoardShedVaccineAnimalsQuery{
				CommandBoardDrilldownQuery: domain.CommandBoardDrilldownQuery{TenantID: tenant, Limit: 50},
				ShedID:                     c.ShedID, PartitionLabel: c.PartitionLabel, VaccineCode: c.VaccineCode,
			})
		})
	}

	// The worst cohort cell: most exceptions.
	cohorts := append([]domain.CommandBoardCohortCell(nil), board.CohortMatrix...)
	sort.Slice(cohorts, func(i, j int) bool { return cohorts[i].MissingPriorDoseCount > cohorts[j].MissingPriorDoseCount })
	if len(cohorts) > 0 {
		c := cohorts[0]
		fmt.Printf("  worst cohort cell: %s %s/%s %s exceptions=%d\n", c.Cohort.ParkName, c.Cohort.ManagementStage, c.Cohort.Sex, c.VaccineLabel, c.MissingPriorDoseCount)
		q := domain.CommandBoardCohortCellQuery{
			CommandBoardDrilldownQuery: domain.CommandBoardDrilldownQuery{TenantID: tenant, Limit: 50},
			CohortParkID:               c.Cohort.ParkID, ManagementStage: c.Cohort.ManagementStage,
			Sex: c.Cohort.Sex, DoseCodes: c.DoseCodes,
		}
		run("  cohort exceptions (page 50)", func() (any, error) { return repo.CommandBoardCohortExceptions(ctx, q) })
		run("  cohort administered days", func() (any, error) { return repo.CommandBoardCohortDays(ctx, q) })
	}
}
