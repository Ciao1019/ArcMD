// ArcMD is a Markdown editor in the manner of Arc and Zen, drawn in native
// UI by MyGo.
//
//	go run ./cmd/arcmd
//	go run ./cmd/arcmd ask -space Work "why was the release delayed?"
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/Ciao1019/ArcMD/internal/app"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--jsx-check" {
		os.Exit(jsxCheck(os.Args[2:]))
	}
	cfg, err := app.DefaultConfig()
	if err != nil {
		log.Fatal(err)
	}
	// ARCMD_HOME keeps a development run's settings and spaces in one
	// folder, away from the user's own.
	if home := os.Getenv("ARCMD_HOME"); home != "" {
		cfg = app.Config{ConfigDir: filepath.Join(home, "config"), DocumentsDir: filepath.Join(home, "spaces")}
	}
	if len(os.Args) > 1 && os.Args[1] == "ask" {
		os.Exit(ask(os.Args[2:], cfg, os.Stdout, os.Stderr))
	}
	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}
