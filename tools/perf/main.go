// Command perf converts fixtures, prepares timing overlays, runs benchmarks and summarizes them.
package main

import (
	"fmt"
	"os"
)

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage: go run ./tools/perf <convert|overlay|run|summarize> [arguments]")
		fmt.Println("  convert <source.xml> <output-directory>\n  overlay <output-directory>\n  run --quiet-repo <path> --library <path> --out <new-directory>\n  summarize --out <report.json> <run-directory>...")
		return nil
	}
	switch args[0] {
	case "convert":
		if len(args) != 3 {
			return fmt.Errorf("convert requires source XML and destination directory")
		}
		manifest, err := convert(args[1], args[2])
		if err != nil {
			return err
		}
		return printJSON(manifest)
	case "overlay":
		if len(args) != 2 {
			return fmt.Errorf("overlay requires an output directory")
		}
		return overlayCommand(args[1])
	case "run":
		return benchmarkCommand(args[1:])
	case "summarize":
		return summarizeCommand(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Performance tool:", err)
		os.Exit(1)
	}
}
