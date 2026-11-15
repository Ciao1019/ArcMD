// Command readme-art builds all localized README vector animations
// (hero, flow, themes, annotations, bookmarks, library, sidebar) and the
// accompanying annotation demo documents from a single unified ArcMD window
// shell. Run from the repository root:
//
//	CGO_ENABLED=0 go run ./tools/readme-art
package main

import (
	"fmt"
	"os"
)

func main() {
	steps := []func() error{
		buildHero,
		buildFlow,
		buildThemes,
		buildAnnotations,
		buildBookmarks,
		buildLibrary,
		buildSidebar,
	}
	for _, fn := range steps {
		if err := fn(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
