package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const styleConfigPollInterval = 1500 * time.Millisecond

// styleConfigState tracks live loading and mtime polling for the selected style.json,
// merged over any selected built-in paper preset.
type styleConfigState struct {
	loaded       bool
	lastCheck    time.Time
	portablePath string
	loadedPath   string
	modNano      int64
	size         int64
	exists       bool
	errPath      string
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

func dirWithin(base, target string) bool {
	if base == "" || target == "" {
		return false
	}
	baseClean := filepath.Clean(base)
	targetClean := filepath.Clean(target)
	rel, err := filepath.Rel(baseClean, targetClean)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isUnsafePortableDir(dir string) bool {
	clean := filepath.Clean(dir)
	slash := strings.ToLower(filepath.ToSlash(clean))
	if strings.Contains(slash, ".app/contents/macos") {
		return true
	}
	switch slash {
	case "/", "/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin", "/opt/homebrew/bin", "/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp":
		return true
	}
	if tmp := os.TempDir(); tmp != "" {
		if dirWithin(tmp, clean) {
			return true
		}
		if evalTmp, err := filepath.EvalSymlinks(tmp); err == nil && dirWithin(evalTmp, clean) {
			return true
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		downloads := filepath.Join(home, "Downloads")
		if filepath.Clean(downloads) == clean {
			return true
		}
	}
	return false
}

func portableStylePathForExecutable(executable string) string {
	if os.Getenv("ARCMD_HOME") != "" || executable == "" {
		return ""
	}
	info, err := os.Lstat(executable)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	dir := filepath.Dir(executable)
	if isUnsafePortableDir(dir) {
		return ""
	}
	if evalDir, err := filepath.EvalSymlinks(dir); err == nil && isUnsafePortableDir(evalDir) {
		return ""
	}
	return filepath.Join(dir, prefs.StyleFileName)
}

func discoverPortableStylePath() string {
	if os.Getenv("ARCMD_HOME") != "" {
		return ""
	}
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return portableStylePathForExecutable(executable)
}

func discoverPortableStylePathFor(cfg Config) string {
	if os.Getenv("ARCMD_HOME") != "" {
		return ""
	}
	if def, err := DefaultConfig(); err == nil && cfg.ConfigDir != "" && filepath.Clean(cfg.ConfigDir) != filepath.Clean(def.ConfigDir) {
		return ""
	}
	return discoverPortableStylePath()
}

func (a *App) styleConfigPath() string {
	if portable := a.styleCfg.portablePath; portable != "" {
		if info, err := os.Lstat(portable); err == nil && info.Mode().IsRegular() {
			return portable
		}
	}
	return a.userStyleConfigPath()
}

func (a *App) userStyleConfigPath() string {
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
	if a.styleCfg.portablePath == "" && a.userStyleConfigPath() == "" {
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
	pathChanged := p != a.styleCfg.loadedPath
	info, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		a.styleCfg.active = nil
		a.styleCfg.mergedValid = false
		a.styleCfg.loadedPath = ""
		a.styleCfg.exists = false
		a.styleCfg.modNano, a.styleCfg.size = 0, 0
		a.styleCfg.errPath = ""
		a.styleCfg.errModNano, a.styleCfg.errSize = 0, 0
		return true
	}
	if err != nil {
		if pathChanged {
			a.styleCfg.active = nil
			a.styleCfg.mergedValid = false
			a.styleCfg.loadedPath = p
		}
		if force {
			a.notify("读取样式配置文件失败。%v", err)
		}
		return false
	}
	modNano, size := info.ModTime().UnixNano(), info.Size()
	if !force && !pathChanged && a.styleCfg.exists && modNano == a.styleCfg.modNano && size == a.styleCfg.size {
		return a.styleCfg.errModNano == 0 && a.styleCfg.errSize == 0
	}
	cfg, loadErr := prefs.LoadStyleFile(p)
	a.styleCfg.loadedPath = p
	a.styleCfg.exists = true
	a.styleCfg.modNano, a.styleCfg.size = modNano, size
	if loadErr != nil {
		if pathChanged {
			a.styleCfg.active = nil
			a.styleCfg.mergedValid = false
		}
		if force || a.styleCfg.errPath != p || a.styleCfg.errModNano != modNano || a.styleCfg.errSize != size {
			a.styleCfg.errPath = p
			a.styleCfg.errModNano, a.styleCfg.errSize = modNano, size
			a.notify("样式配置文件 style.json 有误：%v", loadErr)
		}
		return false
	}
	a.styleCfg.active = cfg
	a.styleCfg.mergedValid = false
	a.styleCfg.errPath = ""
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
	if p == a.styleCfg.portablePath {
		if f, err := os.OpenFile(p, os.O_WRONLY, 0); err != nil {
			userPath := a.userStyleConfigPath()
			if userPath != "" && userPath != p {
				a.notify("可执行文件目录中的 style.json 为只读，已改为打开用户配置目录中的 style.json。")
				p = userPath
			}
		} else {
			_ = f.Close()
		}
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
