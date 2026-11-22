//go:build !linux

package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func newAppWindow(opts mygo.WindowOptions, view func(*ui.Context)) *mygo.Window {
	opts.Content = ui.View(view)
	return mygo.NewWindow(opts)
}

func hasAppWindowControls() bool { return false }
