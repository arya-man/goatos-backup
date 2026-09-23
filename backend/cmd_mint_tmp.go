package main

import (
	"fmt"
	"os"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/auth"
)

func main() {
	tok, err := auth.MintHS256Token(auth.Config{
		Issuer:   "goatos-local",
		Audience: "goatos-api",
		Secret:   []byte("goatos-local-dev-secret-32-bytes-min"),
		MaxTTL:   24 * time.Hour,
	}, os.Args[1], "00000000-0000-4000-8000-000000000001", 12*time.Hour)
	if err != nil {
		panic(err)
	}
	fmt.Print(tok)
}
