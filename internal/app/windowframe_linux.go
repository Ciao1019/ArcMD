package app

import (
	"os"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var kdeWindowState = sync.OnceValue(func() func(uintptr) uint32 {
	// GTK reports the compositor's default, not whether it accepts CSD.
	if !strings.Contains(":"+strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))+":", ":kde:") {
		return nil
	}
	gdk, err := purego.Dlopen("libgdk-3.so.0", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	var displayDefault, screenDefault func() uintptr
	var rgbaVisual func(uintptr) uintptr
	var composited func(uintptr) int32
	var state func(uintptr) uint32
	purego.RegisterLibFunc(&displayDefault, gdk, "gdk_display_get_default")
	purego.RegisterLibFunc(&screenDefault, gdk, "gdk_screen_get_default")
	purego.RegisterLibFunc(&rgbaVisual, gdk, "gdk_screen_get_rgba_visual")
	purego.RegisterLibFunc(&composited, gdk, "gdk_screen_is_composited")
	purego.RegisterLibFunc(&state, gdk, "gdk_window_get_state")
	display, screen := displayDefault(), screenDefault()
	if display == 0 || screen == 0 || rgbaVisual(screen) == 0 || composited(screen) == 0 {
		return nil
	}
	typ, err := purego.Dlsym(gdk, "gdk_wayland_display_get_type")
	if err != nil {
		return nil
	}
	var waylandType func() uintptr
	var isType func(uintptr, uintptr) int32
	purego.RegisterFunc(&waylandType, typ)
	purego.RegisterLibFunc(&isType, gdk, "g_type_check_instance_is_a")
	if isType(display, waylandType()) == 0 {
		return nil
	}
	sym, err := purego.Dlsym(gdk, "gdk_wayland_display_prefers_ssd")
	if err != nil {
		return nil
	}
	var prefersSSD func(uintptr) int32
	purego.RegisterFunc(&prefersSSD, sym)
	if prefersSSD(display) == 0 {
		return nil
	}
	gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	var widgetWindow func(uintptr) uintptr
	purego.RegisterLibFunc(&widgetWindow, gtk, "gtk_widget_get_window")
	return func(widget uintptr) uint32 {
		if widget != 0 {
			if window := widgetWindow(widget); window != 0 {
				return state(window)
			}
		}
		return 0
	}
})

func newAppWindow(opts mygo.WindowOptions, view func(*ui.Context)) *mygo.Window {
	state := kdeWindowState()
	if state == nil {
		opts.Content = ui.View(view)
		return mygo.NewWindow(opts)
	}
	// Frameless requests client decorations on KWin before mapping.
	opts.Transparent = true
	opts.Frameless = true
	// Keep the native menu accessible without a permanent row.
	opts.AutoHideMenuBar = true
	var widget uintptr
	opts.Content = ui.View(func(c *ui.Context) {
		const attached = 1<<2 | 1<<4 | 1<<8 | 1<<9 | 1<<11 | 1<<13 | 1<<15 // maximized, fullscreen, tiled edges
		var radius float32
		status := state(widget)
		if status&attached == 0 {
			radius = 12
		}
		c.Root().Radius(radius).Clip()
		view(c)
		ui.Box(c.Key("window-frame-outline")).Absolute().Top(0).Right(0).Bottom(0).Left(0).
			Radius(radius).Border(1, separator(c.Theme())).PassThrough()
	})
	win := mygo.NewWindow(opts)
	win.OnShow(func() {
		widget = win.NativeHandle()
		win.Invalidate()
	})
	return win
}

func hasAppWindowControls() bool {
	return kdeWindowState() != nil
}
