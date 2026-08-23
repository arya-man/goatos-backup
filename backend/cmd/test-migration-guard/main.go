package main

import (
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/migrationguard"
)

func main() {
	// Get binary migration version
	binaryVer, err := migrationguard.BinaryVersion()
	if err != nil {
		fmt.Printf("Error getting binary version: %v\n", err)
		return
	}
	fmt.Printf("Binary migration version: %s\n", binaryVer)
	fmt.Println()
	fmt.Println("=== HAPPY PATH ===")
	fmt.Println("When database matches binary migration version:")
	status, err := migrationguard.Check(binaryVer, binaryVer)
	if err != nil {
		fmt.Printf("RESULT: REFUSED - %v\n", err)
	} else {
		fmt.Printf("RESULT: ALLOWED (bridge starts successfully)\n")
	}

	fmt.Println()
	fmt.Println("=== DRIFT PATH 1: Database behind binary (BinaryAhead) ===")
	fmt.Println("When database has not been migrated to current binary version:")
	dbVer := "000199"
	status, err = migrationguard.Check(dbVer, binaryVer)
	if err != nil {
		fmt.Printf("RESULT: REFUSED\n")
		fmt.Printf("  Error message: %v\n", err)
		fmt.Printf("  BinaryAhead: %v (binary knows about migrations DB doesn't have)\n", status.BinaryAhead)
		fmt.Printf("  DBAhead: %v\n", status.DBAhead)
	} else {
		fmt.Printf("RESULT: ALLOWED (unexpected)\n")
	}

	fmt.Println()
	fmt.Println("=== DRIFT PATH 2: Database ahead of binary (DBAhead) ===")
	fmt.Println("When database has been migrated beyond this binary's knowledge:")
	dbVer = "000202"
	status, err = migrationguard.Check(dbVer, binaryVer)
	if err != nil {
		fmt.Printf("RESULT: REFUSED\n")
		fmt.Printf("  Error message: %v\n", err)
		fmt.Printf("  DBAhead: %v (database knows about migrations binary doesn't - STALE BINARY!)\n", status.DBAhead)
		fmt.Printf("  BinaryAhead: %v\n", status.BinaryAhead)
	} else {
		fmt.Printf("RESULT: ALLOWED (unexpected)\n")
	}
}
