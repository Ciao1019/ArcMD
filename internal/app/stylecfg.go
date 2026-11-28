package app

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const styleConfigPollInterval = 1500 * time.Millisecond

// styleConfigState tracks live loading and mtime polling for <configDir>/style.json,
// merged over any selected built-in paper preset.
type styleConfigState struct {
	loaded       bool
	lastCheck    time.Time
	modNano      int64
	size         int64
	exists       bool
	errModNano   int64
	errSize      int64
	active       *prefs.StyleConfig
	mergedPreset string
	mergedFile   *prefs.StyleConfig
	merged       *prefs.StyleConfig
	mergedValid  bool
	// openFile substitutes for the platform file opener in tests.
	openFile func(path string)
}

type paperPresetChoice struct {
	id    string
	label string
}

var paperPresetChoices = []paperPresetChoice{
	{id: "", label: "默认"},
	{id: "warm", label: "暖米"},
	{id: "sepia", label: "宣纸"},
	{id: "sage", label: "竹青"},
	{id: "slate", label: "雾灰"},
}

func (a *App) paperPreset() string {
	if a == nil || a.prefs == nil || !prefs.IsValidPaperPreset(a.prefs.PaperPreset) {
		return ""
	}
	return a.prefs.PaperPreset
}

func (a *App) setPaperPreset(id string) {
	if a.prefs == nil {
		return
	}
	if !prefs.IsValidPaperPreset(id) {
		id = ""
	}
	if a.prefs.PaperPreset == id {
		return
	}
	a.prefs.PaperPreset = id
	a.styleCfg.mergedValid = false
	a.savePrefs()
}

func paperPresetSwatchColors(id string, dark bool) (paper, ink, border ui.Color) {
	th := documentThemeWith(dark, prefs.BuiltinPaperStyle(id))
	return th.Background, th.Text, th.Border
}

func (a *App) styleConfigPath() string {
	if a.prefStore.Path != "" {
		return filepath.Join(filepath.Dir(a.prefStore.Path), prefs.StyleFileName)
	}
	if a.cfg.ConfigDir != "" {
		return filepath.Join(a.cfg.ConfigDir, prefs.StyleFileName)
	}
	return ""
}

func (a *App) ensureStyleConfigLoaded() {
	if a.styleCfg.loaded {
		return
	}
	a.styleCfg.loaded = true
	a.checkStyleConfig(false)
}

func (a *App) activeStyleConfig() *prefs.StyleConfig {
	a.ensureStyleConfigLoaded()
	preset := a.paperPreset()
	fileCfg := a.styleCfg.active
	if a.styleCfg.mergedValid && a.styleCfg.mergedPreset == preset && a.styleCfg.mergedFile == fileCfg {
		return a.styleCfg.merged
	}
	merged := prefs.MergeStyleConfig(prefs.BuiltinPaperStyle(preset), fileCfg)
	a.styleCfg.mergedPreset = preset
	a.styleCfg.mergedFile = fileCfg
	a.styleCfg.merged = merged
	a.styleCfg.mergedValid = true
	return merged
}

func (a *App) documentTheme(dark bool) *ui.Theme {
	return documentThemeWith(dark, a.activeStyleConfig())
}

func (a *App) pageStyle(dark bool) editor.Page {
	return pageStyleWith(dark, a.activeStyleConfig())
}

// styleConfigTick lazily loads style.json on the first frame and polls its
// modification timestamp every 1500ms using c.Now() + c.After without goroutines.
func (a *App) styleConfigTick(c *ui.Context) {
	if a.styleConfigPath() == "" {
		return
	}
	now := c.Now()
	if !a.styleCfg.loaded {
		a.styleCfg.loaded = true
		a.styleCfg.lastCheck = now
		a.checkStyleConfig(false)
		c.After(styleConfigPollInterval)
		return
	}
	if a.styleCfg.lastCheck.IsZero() {
		a.styleCfg.lastCheck = now
		c.After(styleConfigPollInterval)
		return
	}
	elapsed := now.Sub(a.styleCfg.lastCheck)
	if elapsed >= styleConfigPollInterval {
		a.styleCfg.lastCheck = now
		a.checkStyleConfig(false)
		c.After(styleConfigPollInterval)
		return
	}
	wait := styleConfigPollInterval - elapsed
	if wait <= 0 {
		wait = styleConfigPollInterval
	}
	c.After(wait)
}

func (a *App) checkStyleConfig(force bool) bool {
	p := a.styleConfigPath()
	if p == "" {
		return true
	}
	info, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		a.styleCfg.active = nil
		a.styleCfg.mergedValid = false
		a.styleCfg.exists = false
		a.styleCfg.modNano, a.styleCfg.size = 0, 0
		a.styleCfg.errModNano, a.styleCfg.errSize = 0, 0
		return true
	}
	if err != nil {
		if force {
			a.notify("读取样式配置文件失败。%v", err)
		}
		return false
	}
	modNano, size := info.ModTime().UnixNano(), info.Size()
	if !force && a.styleCfg.exists && modNano == a.styleCfg.modNano && size == a.styleCfg.size {
		return a.styleCfg.errModNano == 0 && a.styleCfg.errSize == 0
	}
	cfg, loadErr := prefs.LoadStyleFile(p)
	a.styleCfg.exists = true
	a.styleCfg.modNano, a.styleCfg.size = modNano, size
	if loadErr != nil {
		if force || a.styleCfg.errModNano != modNano || a.styleCfg.errSize != size {
			a.styleCfg.errModNano, a.styleCfg.errSize = modNano, size
			a.notify("样式配置文件 style.json 有误：%v", loadErr)
		}
		return false
	}
	a.styleCfg.active = cfg
	a.styleCfg.mergedValid = false
	a.styleCfg.errModNano, a.styleCfg.errSize = 0, 0
	return true
}

func (a *App) reloadStyleConfigAction() {
	p := a.styleConfigPath()
	if p == "" {
		return
	}
	a.styleCfg.loaded = true
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		a.checkStyleConfig(true)
		a.notify("尚未创建 style.json，当前使用默认样式。")
		return
	}
	if a.checkStyleConfig(true) {
		a.notify("已重新载入样式配置。")
	}
}

func (a *App) openStyleConfigFile() {
	p := a.styleConfigPath()
	if p == "" {
		return
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		if mkErr := os.MkdirAll(filepath.Dir(p), 0o755); mkErr != nil {
			a.notify("创建样式配置文件失败。%v", mkErr)
			return
		}
		if wErr := fsutil.WriteFileAtomic(p, prefs.DefaultStyleTemplateJSON(), 0o644); wErr != nil {
			a.notify("创建样式配置文件失败。%v", wErr)
			return
		}
	}
	a.styleCfg.loaded = true
	a.checkStyleConfig(false)
	switch {
	case a.styleCfg.openFile != nil:
		a.styleCfg.openFile(p)
	case a.revealFile != nil:
		a.revealFile(p)
	default:
		mygo.Shell.OpenPath(p)
	}
}
