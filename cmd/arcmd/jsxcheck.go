package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Ciao1019/ArcMD/internal/jsx"
)

func jsxCheck(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: ArcMD --jsx-check file.mdx")
		return 2
	}
	source, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	m, err := jsx.Compile(string(source), jsx.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "moejs:", err)
		return 1
	}
	_ = json.NewEncoder(os.Stdout).Encode(m)
	for _, b := range m.Blocks {
		if b.Error != "" {
			fmt.Fprintln(os.Stderr, b.Error)
			return 1
		}
	}
	if len(m.Diagnostics) > 0 {
		fmt.Fprintln(os.Stderr, m.Diagnostics)
		return 1
	}
	return 0
}
