package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pinyin"
	"github.com/Ciao1019/ArcMD/internal/plugin"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo"
	uiglass "github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

const (
	cloudSyncPluginID        = "dev.arcmd.cloud-sync"
	imageHostPluginID        = "dev.arcmd.image-host"
	defaultPluginRegistryURL = "https://github.com/Ciao1019/arcmd-plugins/releases/download/catalog/index.json"
)

type pluginState struct {
	host          *plugin.WASMHost
	cfg           *plugin.ConfigStore
	instances     map[string]plugin.Instance
	startInstance func(p *plugin.Installed) (plugin.Instance, error)
	cachedConfigs map[string]map[string]any
	httpClient    *http.Client
	sync          bool

	query       string
	status      map[string]*pluginRunState
	enabledBufs map[string]*bool
	install     pluginInstallState
	settings    pluginSettingsState
	sheet       pluginSheetState

	registryURL     string
	remoteCatalog   []plugin.Listing
	registryLoading bool
	registryLoaded  bool
	registryErr     string

	lastAutoSync time.Time
	saveSyncDue  time.Time
	saveSyncArm  bool

	pickFile func(done func(path string))
}

type pluginRunState struct {
	busy   bool
	status string
	err    string
}

type pluginInstallState struct {
	open        bool
	downloading bool
	manifest    *plugin.Manifest
	listing     plugin.Listing
	archive     []byte
	err         string
}

type pluginSettingsState struct {
	open         bool
	pluginID     string
	values       map[string]any
	strBufs      map[string]*string
	boolBufs     map[string]*bool
	segBufs      map[string]*int
	spaceBufs    map[string]*bool
	secretSaved  map[string]bool
	secretSuffix map[string]string
	secretBufs   map[string]*string
	err          string
}

type pluginSheetState struct {
	open     bool
	title    string
	markdown string
	mdOpts   markdown.Options
}

func (a *App) initPlugins() {
	dataDir := filepath.Join(a.cfg.ConfigDir, "plugins-data")
	a.pluginsUI.cfg = &plugin.ConfigStore{Dir: dataDir}
	a.pluginsUI.instances = map[string]plugin.Instance{}
	a.pluginsUI.cachedConfigs = map[string]map[string]any{}
	a.pluginsUI.status = map[string]*pluginRunState{}
	a.pluginsUI.enabledBufs = map[string]*bool{}
	host, err := plugin.NewWASMHost(context.Background(), plugin.HostConfig{
		DataDir: dataDir,
		Config:  a.pluginsUI.cfg,
		Secrets: a.qa.secrets,
		HTTPClient: func() *http.Client {
			if a.pluginsUI.httpClient != nil {
				return a.pluginsUI.httpClient
			}
			return http.DefaultClient
		},
	})
	if err != nil {
		a.notify("初始化插件运行时失败。%v", err)
		return
	}
	a.pluginsUI.host = host
}

func (a *App) closePlugins() {
	for id, inst := range a.pluginsUI.instances {
		_ = inst.Close()
		delete(a.pluginsUI.instances, id)
	}
	if a.pluginsUI.host != nil {
		_ = a.pluginsUI.host.Close()
		a.pluginsUI.host = nil
	}
}

func (a *App) pluginRunState(id string) *pluginRunState {
	if a.pluginsUI.status == nil {
		a.pluginsUI.status = map[string]*pluginRunState{}
	}
	st := a.pluginsUI.status[id]
	if st == nil {
		st = &pluginRunState{}
		a.pluginsUI.status[id] = st
	}
	return st
}

func (a *App) findInstalledPlugin(id string) *plugin.Installed {
	for _, p := range a.installed {
		if p.Manifest.ID == id {
			return p
		}
	}
	return nil
}

func (a *App) syncPluginInstances() {
	if a.pluginsUI.cachedConfigs != nil {
		clear(a.pluginsUI.cachedConfigs)
	}
	active := map[string]bool{}
	for _, p := range a.installed {
		if p.Enabled {
			active[p.Manifest.ID] = true
		}
	}
	for id, inst := range a.pluginsUI.instances {
		if !active[id] {
			_ = inst.Close()
			delete(a.pluginsUI.instances, id)
		}
	}
}

func (a *App) closePluginInstance(id string) {
	if inst, ok := a.pluginsUI.instances[id]; ok {
		_ = inst.Close()
		delete(a.pluginsUI.instances, id)
	}
	if a.pluginsUI.cachedConfigs != nil {
		delete(a.pluginsUI.cachedConfigs, id)
	}
}

func (a *App) ensurePluginInstance(p *plugin.Installed) (plugin.Instance, error) {
	if p == nil || p.Manifest == nil {
		return nil, errors.New("plugin not found")
	}
	if !p.Enabled {
		return nil, fmt.Errorf(i18n.T("插件「%s」已停用。"), i18n.T(p.Manifest.Name))
	}
	if p.Manifest.Runtime != plugin.RuntimeWASM {
		return nil, fmt.Errorf(i18n.T("%s 需要进程插件运行时支持。"), i18n.T(p.Manifest.Name))
	}
	if inst, ok := a.pluginsUI.instances[p.Manifest.ID]; ok {
		return inst, nil
	}
	var (
		inst plugin.Instance
		err  error
	)
	if a.pluginsUI.startInstance != nil {
		inst, err = a.pluginsUI.startInstance(p)
	} else {
		if a.pluginsUI.host == nil {
			return nil, errors.New("WASM plugin runtime is unavailable")
		}
		inst, err = a.pluginsUI.host.Start(context.Background(), p)
	}
	if err != nil {
		return nil, err
	}
	if a.pluginsUI.instances == nil {
		a.pluginsUI.instances = map[string]plugin.Instance{}
	}
	a.pluginsUI.instances[p.Manifest.ID] = inst
	return inst, nil
}

func (a *App) pluginConfig(p *plugin.Installed) map[string]any {
	if p == nil || p.Manifest == nil {
		return nil
	}
	if a.pluginsUI.cachedConfigs == nil {
		a.pluginsUI.cachedConfigs = map[string]map[string]any{}
	}
	if cfg, ok := a.pluginsUI.cachedConfigs[p.Manifest.ID]; ok {
		return cfg
	}
	if a.pluginsUI.cfg == nil {
		return nil
	}
	cfg, err := a.pluginsUI.cfg.Load(p.Manifest)
	if err != nil {
		return nil
	}
	a.pluginsUI.cachedConfigs[p.Manifest.ID] = cfg
	return cfg
}

func (a *App) pluginModalOpen() bool {
	return a.pluginsUI.install.open || a.pluginsUI.settings.open || a.pluginsUI.sheet.open || a.gitChanges.open || a.mergeUI.open
}

func pluginIconSVG(name string) *ui.SVG {
	if name == "cloud" {
		return iconCloud
	}
	if name == "image" {
		return iconImage
	}
	if ic := lucideIcons[name]; ic != nil {
		return ic
	}
	return iconPlugins
}

func (a *App) pluginRegistryURL() string {
	if u := strings.TrimSpace(a.pluginsUI.registryURL); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv("ARCMD_PLUGIN_REGISTRY")); u != "" {
		return u
	}
	if a.win != nil {
		return defaultPluginRegistryURL
	}
	return ""
}

func (a *App) fetchPluginRegistry(force bool) {
	regURL := a.pluginRegistryURL()
	if regURL == "" || a.pluginsUI.registryLoading {
		return
	}
	if a.pluginsUI.registryLoaded && !force {
		return
	}
	a.pluginsUI.registryLoading = true
	a.pluginsUI.registryErr = ""

	client := a.pluginsUI.httpClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	finish := func(idx *plugin.Index, err error) {
		a.pluginsUI.registryLoading = false
		if err != nil {
			a.pluginsUI.registryErr = err.Error()
			return
		}
		a.pluginsUI.registryLoaded = true
		a.pluginsUI.registryErr = ""
		if idx != nil {
			a.pluginsUI.remoteCatalog = idx.Plugins
		} else {
			a.pluginsUI.remoteCatalog = nil
		}
	}

	post := a.asyncPost()
	if a.pluginsUI.sync || post == nil {
		idx, err := plugin.FetchIndex(context.Background(), client, regURL)
		finish(idx, err)
		return
	}
	go func() {
		idx, err := plugin.FetchIndex(context.Background(), client, regURL)
		post(func() { finish(idx, err) })
	}()
}

func pluginPermissionLabel(perm plugin.Permission) string {
	switch perm {
	case plugin.PermDocumentRead:
		return i18n.T("读取当前打开的文档")
	case plugin.PermDocumentWrite:
		return i18n.T("修改当前打开的文档")
	case plugin.PermSpaceRead:
		return i18n.T("读取空间内的文档与附件")
	case plugin.PermSpaceWrite:
		return i18n.T("在空间内写入文件并将删除移到废纸篓")
	case plugin.PermNetwork:
		return i18n.T("访问网络")
	case plugin.PermSecrets:
		return i18n.T("在本机凭据目录读写密码与密钥")
	case plugin.PermUIPanel:
		return i18n.T("在界面中显示扩展面板")
	default:
		if desc, ok := plugin.Permissions[perm]; ok {
			return i18n.T(desc)
		}
		return string(perm)
	}
}

func shortCommandLabel(pluginName, cmdTitle string) string {
	if _, after, ok := strings.Cut(cmdTitle, "："); ok {
		return strings.TrimSpace(after)
	}
	if _, after, ok := strings.Cut(cmdTitle, ": "); ok {
		return strings.TrimSpace(after)
	}
	_ = pluginName
	return cmdTitle
}

// libraryPlugins renders the Library -> Plugins page.
func (a *App) libraryPlugins(c *ui.Context) {
	t := c.Theme()
	ink := libraryInk(t)

	ui.Row(c.Key("plugins-bar")).Height(36).Shrink(0).Margin(10, 8, 4, 7).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Row(c.Key("plugins-search")).Height(36).Grow(1).MinWidth(0).Radius(10).PaddingX(12).Gap(10).
			AlignItems(ui.Center).Background(librarySearch(t)).Children(func() {
			ui.Icon(c, iconSearch).FontSize(14).TextColor(ink.Alpha(0.5))
			ui.TextInput(c.Key("plugins-query"), &a.pluginsUI.query).Grow(1).MinWidth(0).Height(24).Padding(0).
				Placeholder(i18n.T("搜索插件…")).Label(i18n.T("搜索插件")).TextColor(ink.Alpha(0.82)).
				Background(ui.Transparent).Border(0, ui.Transparent).FontSize(13)
		})
		addBtn := ui.Box(c.Key("plugins-install-local")).Size(36, 36).Shrink(0).Radius(10).Center().
			Background(librarySearch(t)).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
			Label(i18n.T("从本地安装插件…")).Tooltip(i18n.T("从本地安装插件 (.zip)"))
		if addBtn.Hovered() {
			addBtn.Background(hoverFill(t))
		}
		addBtn.Children(func() {
			ui.Icon(c, iconPlus).FontSize(15).TextColor(ink.Alpha(0.75))
		})
		if addBtn.Clicked() {
			a.pickLocalPluginArchive()
		}
		if a.pluginRegistryURL() != "" {
			refBtn := ui.Box(c.Key("plugins-refresh")).Size(36, 36).Shrink(0).Radius(10).Center().
				Background(librarySearch(t)).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
				Disabled(a.pluginsUI.registryLoading).
				Label(i18n.T("刷新插件列表")).Tooltip(i18n.T("刷新插件列表"))
			if refBtn.Hovered() && !a.pluginsUI.registryLoading {
				refBtn.Background(hoverFill(t))
			}
			refCol := ink.Alpha(0.75)
			if a.pluginsUI.registryLoading {
				refCol = ink.Alpha(0.35)
			}
			refBtn.Children(func() {
				ui.Icon(c, iconRefresh).FontSize(15).TextColor(refCol)
			})
			if refBtn.Clicked() {
				a.fetchPluginRegistry(true)
			}
		}
	})

	words := strings.Fields(strings.ToLower(a.pluginsUI.query))
	matches := func(name, desc, id string) bool {
		if len(words) == 0 {
			return true
		}
		hay := strings.ToLower(name + " " + i18n.T(name) + " " + desc + " " + i18n.T(desc) + " " + id)
		for _, w := range words {
			if !pinyin.ContainsToken(hay, w) {
				return false
			}
		}
		return true
	}

	var shownInstalled []*plugin.Installed
	installedIDs := make(map[string]bool, len(a.installed))
	for _, p := range a.installed {
		installedIDs[p.Manifest.ID] = true
		if matches(p.Manifest.Name, p.Manifest.Description, p.Manifest.ID) {
			shownInstalled = append(shownInstalled, p)
		}
	}

	var shownRemote []plugin.Listing
	for _, listing := range a.pluginsUI.remoteCatalog {
		if installedIDs[listing.ID] {
			continue
		}
		if matches(listing.Name, listing.Description, listing.ID) {
			shownRemote = append(shownRemote, listing)
		}
	}

	hasCatalogSection := len(shownRemote) > 0 || a.pluginsUI.registryLoading || a.pluginsUI.registryErr != ""
	if len(shownInstalled) == 0 && len(a.brokenPlugins) == 0 && !hasCatalogSection {
		if len(words) > 0 {
			ui.Text(c, i18n.T("没有符合搜索的插件")).FontSize(13).TextColor(ink.Alpha(0.55)).
				AlignSelf(ui.Center).Margin(24, 0, 0, 0)
			return
		}
		libraryEmpty(c, iconPlugins, i18n.T("暂无插件"), i18n.T("点击右上角 + 安装本地 .zip 插件包。"))
		return
	}

	ui.Scroll(c.Key("plugins-list")).Grow(1).MinHeight(0).Padding(4, 8, 12, 8).Gap(8).Children(func() {
		if len(shownInstalled) > 0 {
			ui.Text(c, i18n.T("已安装")).FontSize(12).FontWeight(600).TextColor(ink.Alpha(0.6)).Padding(4, 4, 0, 4)
			for _, p := range shownInstalled {
				a.installedPluginCard(c, p)
			}
		}
		if len(a.brokenPlugins) > 0 && len(words) == 0 {
			ui.Text(c, i18n.T("损毁的插件")).FontSize(12).FontWeight(600).TextColor(t.Danger).Padding(6, 4, 0, 4)
			for _, b := range a.brokenPlugins {
				card := ui.Column(c.Key("broken-"+filepath.Base(b.Dir))).Padding(10, 12).Gap(4).Radius(14).
					Material(uiglass.Blur{Radius: 16}).
					Shadow(0, 6, 18, -4, pluginCardShadow(t)).
					Border(1, t.Danger.Alpha(0.35))
				card.Draw(func(pt *ui.Painter, r ui.Rect) { paintPluginGlassCard(pt, t, r, 14) })
				card.Children(func() {
					ui.Text(c, filepath.Base(b.Dir)).FontSize(13).FontWeight(600).TextColor(ink.Alpha(0.88))
					ui.Text(c, b.Err.Error()).FontSize(11).TextColor(t.Danger)
				})
			}
		}
		if hasCatalogSection {
			ui.Text(c, i18n.T("官方插件")).FontSize(12).FontWeight(600).TextColor(ink.Alpha(0.6)).Padding(6, 4, 0, 4)
			for _, listing := range shownRemote {
				a.remoteCatalogPluginCard(c, listing)
			}
			if a.pluginsUI.registryLoading {
				ui.Text(c, i18n.T("正在获取远程插件列表…")).FontSize(12).TextColor(ink.Alpha(0.55)).Padding(2, 4)
			} else if a.pluginsUI.registryErr != "" {
				ui.Text(c, fmt.Sprintf(i18n.T("获取插件市场失败：%v"), a.pluginsUI.registryErr)).
					FontSize(12).TextColor(t.Danger.Alpha(0.85)).Padding(2, 4)
			}
		}
	})
}

func pluginGlassCard(c *ui.Context, key string) ui.Element {
	t := c.Theme()
	card := ui.Column(c.Key(key)).Padding(12, 13).Gap(9).Radius(14).
		Material(uiglass.Blur{Radius: 18}).
		BorderWidth(1).BorderLinearGradient(pluginCardRimGradient(t)).
		Shadow(0, 6, 18, -4, pluginCardShadow(t))
	card.Draw(func(pt *ui.Painter, r ui.Rect) {
		paintPluginGlassCard(pt, t, r, 14)
	})
	return card
}

func pluginGlassIconTile(c *ui.Context, icon *ui.SVG, color ui.Color) {
	t := c.Theme()
	ui.Box(c).Size(34, 34).Shrink(0).Radius(9).Center().
		LinearGradient(pluginTileSheen(t)).
		BorderWidth(1).BorderLinearGradient(pluginTileRim(t)).
		Shadow(0, 2, 6, -1, pluginCardShadow(t)).Children(func() {
		ui.Icon(c, icon).FontSize(16).TextColor(color)
	})
}

func pluginGlassButton(c *ui.Context, key, text, label string, disabled bool) ui.Element {
	t := c.Theme()
	ink := libraryInk(t)
	if label == "" {
		label = text
	}
	b := ui.Row(c.Key(key)).Height(28).PaddingX(12).Radius(9).Center().
		Focusable().Role(ui.RoleButton).Label(label).Disabled(disabled)
	textCol := ink.Alpha(0.86)
	if disabled {
		b.LinearGradient(pluginButtonSheen(t, false, false)).Opacity(0.5).
			BorderWidth(1).BorderLinearGradient(pluginButtonRimGradient(t))
		textCol = ink.Alpha(0.5)
	} else {
		b.Cursor(ui.CursorPointer)
		b.LinearGradient(pluginButtonSheen(t, b.Hovered(), b.Pressed())).
			BorderWidth(1).BorderLinearGradient(pluginButtonRimGradient(t)).
			Shadow(0, 2, 5, -1, pluginCardShadow(t))
	}
	b.Children(func() {
		ui.Text(c, text).FontSize(12.5).FontWeight(500).TextColor(textCol).NoWrap()
	})
	return b
}

func (a *App) installedPluginCard(c *ui.Context, p *plugin.Installed) {
	t := c.Theme()
	ink := libraryInk(t)
	m := p.Manifest
	name := i18n.T(m.Name)
	st := a.pluginRunState(m.ID)

	pluginGlassCard(c, "installed-"+m.ID).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			iconCol := ink.Alpha(0.45)
			if p.Enabled {
				iconCol = t.Accent
			}
			pluginGlassIconTile(c, pluginIconSVG(m.Icon), iconCol)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1.5).Children(func() {
				ui.Text(c, name).FontSize(14).FontWeight(600).TextColor(ink.Alpha(0.92)).SingleLine()
				ui.Text(c, "v"+m.Version).FontSize(11).TextColor(ink.Alpha(0.52)).SingleLine()
			})
			if a.pluginsUI.enabledBufs == nil {
				a.pluginsUI.enabledBufs = map[string]*bool{}
			}
			buf := a.pluginsUI.enabledBufs[m.ID]
			if buf == nil {
				v := p.Enabled
				buf = &v
				a.pluginsUI.enabledBufs[m.ID] = buf
			} else {
				*buf = p.Enabled
			}
			ui.Switch(c.Key("toggle-"+m.ID), buf).Label(i18n.T("启用 ") + name)
			if *buf != p.Enabled {
				a.setPluginEnabled(m.ID, *buf)
			}
		})
		if m.Description != "" {
			ui.Text(c, i18n.T(m.Description)).FontSize(12).LineHeight(1.48).TextColor(ink.Alpha(0.72))
		}
		switch {
		case st.busy:
			ui.Text(c, i18n.T("正在执行…")).FontSize(12).TextColor(t.Accent)
		case st.err != "":
			ui.Text(c, st.err).FontSize(12).TextColor(t.Danger)
		case st.status != "":
			ui.Text(c, st.status).FontSize(12).TextColor(ink.Alpha(0.65))
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if p.Enabled {
				for _, pc := range m.Contributes.Commands {
					if !pc.Primary {
						continue
					}
					cmd := pc
					btnLabel := i18n.T(shortCommandLabel(m.Name, cmd.Title))
					if pluginGlassButton(c, "primary-"+m.ID+"-"+cmd.ID, btnLabel, "", st.busy).Clicked() {
						a.runPluginCommand(p, cmd.ID, nil, false)
					}
					break
				}
				if m.ID == cloudSyncPluginID {
					if pluginGlassButton(c, "git-"+m.ID, i18n.T("变更"), i18n.T("查看变更与冲突"), false).Clicked() {
						a.openGitChanges()
					}
				}
				if len(m.Contributes.Settings) > 0 {
					if pluginGlassButton(c, "settings-"+m.ID, i18n.T("设置…"), name+" "+i18n.T("设置"), false).Clicked() {
						a.openPluginSettings(m.ID)
					}
				}
			} else {
				ui.Text(c, i18n.T("已停用")).FontSize(12).TextColor(ink.Alpha(0.45))
			}
			ui.Spacer(c)
			iconButton(c, "menu-"+m.ID, iconEllipsis, name+i18n.T(" 的菜单"), i18n.T("更多"), false).Size(28, 28).
				PopoverMenuButton(func(menu *ui.Menu) {
					if p.Enabled {
						for _, pc := range m.Contributes.Commands {
							cmd := pc
							if menu.Item(i18n.T(cmd.Title)).Disabled(st.busy).Chosen() {
								a.runPluginCommand(p, cmd.ID, nil, false)
							}
						}
						if len(m.Contributes.Settings) > 0 {
							if menu.Item(i18n.T("设置…")).Chosen() {
								a.openPluginSettings(m.ID)
							}
						}
						menu.Separator()
					}
					if menu.Item(i18n.T("在访达中显示")).Chosen() {
						revealDir(p.Dir)
					}
					if menu.Item(i18n.T("卸载插件")).Chosen() {
						a.uninstallPlugin(m.ID)
					}
				})
		})
	})
}

func (a *App) remoteCatalogPluginCard(c *ui.Context, listing plugin.Listing) {
	a.catalogPluginCardView(c, listing.ID, listing.Name, listing.Version, listing.Author, listing.Description, listing.Icon, func() {
		a.promptInstallRemotePlugin(listing)
	})
}

func (a *App) catalogPluginCardView(c *ui.Context, id, rawName, version, author, desc, icon string, onInstall func()) {
	ink := libraryInk(c.Theme())
	name := i18n.T(rawName)

	pluginGlassCard(c, "catalog-"+id).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			pluginGlassIconTile(c, pluginIconSVG(icon), c.Theme().Accent)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1.5).Children(func() {
				ui.Text(c, name).FontSize(14).FontWeight(600).TextColor(ink.Alpha(0.92)).SingleLine()
				meta := "v" + version
				if author != "" {
					meta += " · " + author
				}
				ui.Text(c, meta).FontSize(11).TextColor(ink.Alpha(0.54)).SingleLine()
			})
			if pluginGlassButton(c, "install-"+id, i18n.T("安装"), i18n.T("安装 ")+name, false).Clicked() {
				onInstall()
			}
		})
		if desc != "" {
			ui.Text(c, i18n.T(desc)).FontSize(12).LineHeight(1.48).TextColor(ink.Alpha(0.72))
		}
	})
}

func (a *App) promptInstallPlugin(man *plugin.Manifest, listing plugin.Listing, archive []byte) {
	a.pluginsUI.install = pluginInstallState{
		open:     true,
		manifest: man,
		listing:  listing,
		archive:  archive,
	}
}

func (a *App) promptInstallRemotePlugin(listing plugin.Listing) {
	a.pluginsUI.install = pluginInstallState{
		open: true,
		manifest: &plugin.Manifest{
			ID:          listing.ID,
			Name:        listing.Name,
			Version:     listing.Version,
			Description: listing.Description,
			Author:      listing.Author,
			Homepage:    listing.Homepage,
			Icon:        listing.Icon,
			Runtime:     plugin.RuntimeWASM,
			Permissions: listing.Permissions,
		},
		listing: listing,
	}
}

func (a *App) pickLocalPluginArchive() {
	openArchive := func(filePath string) {
		if filePath == "" {
			return
		}
		b, err := os.ReadFile(filePath)
		if err != nil {
			a.notify("读取插件包失败。%v", err)
			return
		}
		man, shaHex, err := plugin.InspectArchive(b)
		if err != nil {
			a.notify("无效的插件包。%v", err)
			return
		}
		a.promptInstallPlugin(man, plugin.Listing{
			ID:          man.ID,
			Name:        man.Name,
			Version:     man.Version,
			Description: man.Description,
			Author:      man.Author,
			Icon:        man.Icon,
			Permissions: man.Permissions,
			SHA256:      shaHex,
		}, b)
	}
	if a.pluginsUI.pickFile != nil {
		a.pluginsUI.pickFile(openArchive)
		return
	}
	win := a.win
	opts := mygo.OpenDialogOptions{
		Parent: win,
		Title:  i18n.T("选择插件安装包 (.zip)"),
		Filters: []mygo.FileFilter{
			{Name: i18n.T("插件包"), Extensions: []string{"zip"}},
		},
	}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if err != nil || len(paths) == 0 {
			return
		}
		win.Update(func() { openArchive(paths[0]) })
	}()
}

func (a *App) confirmInstallPlugin() {
	st := &a.pluginsUI.install
	if st.manifest == nil || st.downloading {
		return
	}
	finish := func(inst *plugin.Installed, err error) {
		st.downloading = false
		if err != nil {
			st.err = err.Error()
			return
		}
		if err := a.plugins.SetEnabled(inst.Manifest.ID, true); err != nil {
			st.err = err.Error()
			return
		}
		id := inst.Manifest.ID
		hasSettings := len(inst.Manifest.Contributes.Settings) > 0
		name := i18n.T(inst.Manifest.Name)
		*st = pluginInstallState{}
		a.closePluginInstance(id)
		a.reloadPlugins()
		a.notify("已安装插件「%s」。", name)
		if hasSettings {
			a.openPluginSettings(id)
		}
	}

	if len(st.archive) > 0 {
		inst, err := a.plugins.InstallArchive(st.archive, st.listing)
		finish(inst, err)
		return
	}
	if st.listing.Download == "" {
		return
	}
	st.downloading = true
	st.err = ""
	listing := st.listing
	client := a.pluginsUI.httpClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	post := a.asyncPost()
	if a.pluginsUI.sync || post == nil {
		inst, err := a.plugins.Install(context.Background(), client, listing)
		finish(inst, err)
		return
	}
	go func() {
		inst, err := a.plugins.Install(context.Background(), client, listing)
		post(func() {
			if !st.open || st.manifest == nil || st.manifest.ID != listing.ID {
				return
			}
			finish(inst, err)
		})
	}()
}

func (a *App) setPluginEnabled(id string, on bool) {
	if err := a.plugins.SetEnabled(id, on); err != nil {
		a.notify("更新插件状态失败。%v", err)
		return
	}
	if !on {
		a.closePluginInstance(id)
	}
	a.reloadPlugins()
}

func (a *App) uninstallPlugin(id string) {
	p := a.findInstalledPlugin(id)
	name := id
	if p != nil {
		name = i18n.T(p.Manifest.Name)
	}
	a.closePluginInstance(id)
	if err := a.plugins.Uninstall(id); err != nil {
		a.notify("卸载插件失败。%v", err)
		return
	}
	if a.pluginsUI.settings.open && a.pluginsUI.settings.pluginID == id {
		a.pluginsUI.settings = pluginSettingsState{}
	}
	delete(a.pluginsUI.status, id)
	a.reloadPlugins()
	a.notify("已卸载插件「%s」。", name)
}

func (a *App) openPluginSettings(id string) {
	p := a.findInstalledPlugin(id)
	if p == nil || p.Manifest == nil {
		return
	}
	vals, err := a.pluginsUI.cfg.Load(p.Manifest)
	if err != nil {
		a.notify("读取插件设置失败。%v", err)
		vals = map[string]any{}
	}
	ss := pluginSettingsState{
		open:         true,
		pluginID:     id,
		values:       vals,
		strBufs:      map[string]*string{},
		boolBufs:     map[string]*bool{},
		segBufs:      map[string]*int{},
		spaceBufs:    map[string]*bool{},
		secretSaved:  map[string]bool{},
		secretSuffix: map[string]string{},
		secretBufs:   map[string]*string{},
	}
	for _, f := range p.Manifest.Contributes.Settings {
		switch f.Type {
		case "secret":
			empty := ""
			ss.secretBufs[f.ID] = &empty
			if sk, serr := plugin.SecretKey(id, f.ID); serr == nil && a.qa.secrets != nil {
				if raw, gerr := a.qa.secrets.Get(sk); gerr == nil && len(raw) > 0 {
					ss.secretSaved[f.ID] = true
					ss.secretSuffix[f.ID] = lastRunes(string(raw), 4)
				}
			}
		case "string", "select":
			s := fmt.Sprint(vals[f.ID])
			if vals[f.ID] == nil {
				s = ""
			}
			ss.strBufs[f.ID] = &s
		case "bool":
			b, _ := vals[f.ID].(bool)
			ss.boolBufs[f.ID] = &b
		case "segmented":
			cur := fmt.Sprint(vals[f.ID])
			idx := 0
			for i, o := range f.Options {
				if o.Value == cur {
					idx = i
					break
				}
			}
			ss.segBufs[f.ID] = &idx
		case "spaces":
			selected := stringSliceValue(vals[f.ID])
			for _, sp := range a.state.Spaces {
				if sp.IsExternal() {
					continue
				}
				on := slices.Contains(selected, sp.ID)
				ss.spaceBufs[sp.ID] = &on
			}
		}
	}
	a.pluginsUI.settings = ss
}

func (a *App) savePluginSettings() {
	ss := &a.pluginsUI.settings
	p := a.findInstalledPlugin(ss.pluginID)
	if p == nil || p.Manifest == nil || a.pluginsUI.cfg == nil {
		return
	}
	for _, f := range p.Manifest.Contributes.Settings {
		switch f.Type {
		case "string", "select":
			if ptr := ss.strBufs[f.ID]; ptr != nil {
				ss.values[f.ID] = *ptr
			}
		case "bool":
			if ptr := ss.boolBufs[f.ID]; ptr != nil {
				ss.values[f.ID] = *ptr
			}
		case "segmented":
			if ptr := ss.segBufs[f.ID]; ptr != nil && *ptr >= 0 && *ptr < len(f.Options) {
				ss.values[f.ID] = f.Options[*ptr].Value
			}
		case "spaces":
			var ids []string
			for _, sp := range a.state.Spaces {
				if sp.IsExternal() {
					continue
				}
				if ptr := ss.spaceBufs[sp.ID]; ptr != nil && *ptr {
					ids = append(ids, sp.ID)
				}
			}
			ss.values[f.ID] = ids
		}
	}
	if err := a.pluginsUI.cfg.Save(p.Manifest, ss.values); err != nil {
		ss.err = err.Error()
		return
	}
	ss.err = ""
	if a.pluginsUI.cachedConfigs == nil {
		a.pluginsUI.cachedConfigs = map[string]map[string]any{}
	}
	a.pluginsUI.cachedConfigs[p.Manifest.ID] = maps.Clone(ss.values)
}

func (a *App) savePluginSecret(fieldID string) {
	ss := &a.pluginsUI.settings
	ptr := ss.secretBufs[fieldID]
	if ptr == nil {
		return
	}
	val := strings.TrimSpace(*ptr)
	if val == "" {
		return
	}
	sk, err := plugin.SecretKey(ss.pluginID, fieldID)
	if err != nil {
		ss.err = err.Error()
		return
	}
	if a.qa.secrets == nil {
		ss.err = "secret store unavailable"
		return
	}
	if err := a.qa.secrets.Set(sk, []byte(val)); err != nil {
		ss.err = err.Error()
		return
	}
	ss.secretSaved[fieldID] = true
	ss.secretSuffix[fieldID] = lastRunes(val, 4)
	*ptr = ""
	ss.err = ""
}

func (a *App) savePendingPluginSecrets() {
	ss := &a.pluginsUI.settings
	if !ss.open {
		return
	}
	for id, ptr := range ss.secretBufs {
		if ptr != nil && strings.TrimSpace(*ptr) != "" {
			a.savePluginSecret(id)
		}
	}
}

func (a *App) removePluginSecret(fieldID string) {
	ss := &a.pluginsUI.settings
	sk, err := plugin.SecretKey(ss.pluginID, fieldID)
	if err != nil {
		return
	}
	if a.qa.secrets != nil {
		if err := a.qa.secrets.Delete(sk); err != nil {
			ss.err = err.Error()
			return
		}
	}
	delete(ss.secretSaved, fieldID)
	delete(ss.secretSuffix, fieldID)
	if ptr := ss.secretBufs[fieldID]; ptr != nil {
		*ptr = ""
	}
}

func (a *App) pluginInstallConfirmView(c *ui.Context) {
	st := &a.pluginsUI.install
	if !st.open || st.manifest == nil {
		return
	}
	m := st.manifest
	name := i18n.T(m.Name)
	a.spaceModal(c, "plugin-install-confirm", &st.open, func(t *ui.Theme) {
		ui.Column(c.Key("plugin-install-body")).Width(420).Gap(14).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(36, 36).Shrink(0).Radius(9).Center().Background(t.Accent.Alpha(0.14)).Children(func() {
					ui.Icon(c, pluginIconSVG(m.Icon)).FontSize(18).TextColor(t.Accent)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, name).FontSize(18).FontWeight(600)
					meta := "v" + m.Version
					if m.Author != "" {
						meta += " · " + m.Author
					}
					ui.Text(c, meta).FontSize(12).TextColor(t.TextMuted)
				})
			})
			if m.Description != "" {
				ui.Text(c, i18n.T(m.Description)).FontSize(13).LineHeight(1.5).TextColor(t.TextMuted)
			}
			settingsGroup(c, "plugin-perms", func() {
				ui.Text(c, i18n.T("此插件请求以下权限：")).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Padding(10, 12, 4, 12)
				if len(m.Permissions) == 0 {
					ui.Text(c, i18n.T("此插件不请求额外系统权限。")).FontSize(13).Padding(4, 12, 10, 12)
					return
				}
				for i, perm := range m.Permissions {
					if i > 0 {
						ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
					}
					ui.Row(c.Key(string(perm))).Padding(8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconCheck).FontSize(13).TextColor(t.Accent)
						ui.Text(c, pluginPermissionLabel(perm)).FontSize(13).Grow(1).MinWidth(0)
					})
				}
			})
			if m.Runtime == plugin.RuntimeProcess {
				ui.Text(c, i18n.T("注意：此插件以本机进程运行，拥有你的系统用户权限。")).FontSize(12).TextColor(t.Danger)
			}
			if st.err != "" {
				ui.Text(c, st.err).FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("cancel"), i18n.T("取消")).Clicked() {
					*st = pluginInstallState{}
				}
				label := i18n.T("安装并启用")
				if len(st.archive) == 0 && st.listing.Download != "" {
					label = i18n.T("下载并安装")
					if st.downloading {
						label = i18n.T("正在下载…")
					}
				}
				if ui.PrimaryButton(c.Key("confirm"), label).Disabled(st.downloading).Clicked() {
					a.confirmInstallPlugin()
				}
			})
		})
	})
}

func (a *App) pluginSettingsView(c *ui.Context) {
	ss := &a.pluginsUI.settings
	if !ss.open {
		return
	}
	p := a.findInstalledPlugin(ss.pluginID)
	if p == nil || p.Manifest == nil {
		ss.open = false
		return
	}
	w, h := c.Size()
	m := p.Manifest
	name := i18n.T(m.Name)
	st := a.pluginRunState(m.ID)

	a.spaceModal(c, "plugin-settings-"+m.ID, &ss.open, func(t *ui.Theme) {
		ui.Column(c.Key("plugin-settings-body")).Size(min(540, w-t.Space(20)), min(560, h-t.Space(20))).Gap(12).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(32, 32).Shrink(0).Radius(8).Center().Background(t.Accent.Alpha(0.14)).Children(func() {
					ui.Icon(c, pluginIconSVG(m.Icon)).FontSize(16).TextColor(t.Accent)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, name+i18n.T("设置")).FontSize(18).FontWeight(600)
					ui.Text(c, "v"+m.Version).FontSize(12).TextColor(t.TextMuted)
				})
				if iconButton(c, "close", iconX, i18n.T("关闭"), i18n.T("关闭"), false).Size(28, 28).Clicked() {
					a.savePendingPluginSecrets()
					ss.open = false
				}
			})

			ui.Scroll(c.Key("plugin-settings-scroll")).Grow(1).MinHeight(0).Padding(2, 4, 2, 0).Gap(12).Children(func() {
				type fieldGroup struct {
					name   string
					fields []plugin.SettingField
				}
				var groups []fieldGroup
				groupIndex := map[string]int{}
				for _, f := range m.Contributes.Settings {
					if !whenMatches(f.When, ss.values) {
						continue
					}
					gName := f.Group
					if gName == "" {
						gName = i18n.T("通用")
					}
					idx, ok := groupIndex[gName]
					if !ok {
						idx = len(groups)
						groupIndex[gName] = idx
						groups = append(groups, fieldGroup{name: gName})
					}
					groups[idx].fields = append(groups[idx].fields, f)
				}

				for _, grp := range groups {
					ui.Text(c, i18n.T(grp.name)).FontSize(13).FontWeight(600).TextColor(t.TextMuted).Padding(4, 2, 0, 2)
					settingsGroup(c, "grp-"+grp.name, func() {
						for i, f := range grp.fields {
							if i > 0 {
								ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
							}
							a.pluginSettingRow(c, f)
						}
					})
				}

				if st.err != "" {
					ui.Text(c, st.err).FontSize(12).TextColor(t.Danger).Selectable()
				} else if st.status != "" {
					ui.Text(c, st.status).FontSize(12).TextColor(t.TextMuted)
				}
				if ss.err != "" {
					ui.Text(c, ss.err).FontSize(12).TextColor(t.Danger)
				}
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				for _, pc := range m.Contributes.Commands {
					if !pc.InSettings {
						continue
					}
					cmd := pc
					label := i18n.T(shortCommandLabel(m.Name, cmd.Title))
					if cmd.Primary {
						if ui.PrimaryButton(c.Key("cmd-"+cmd.ID), label).Disabled(st.busy || !p.Enabled).Clicked() {
							a.savePendingPluginSecrets()
							a.savePluginSettings()
							a.runPluginCommand(p, cmd.ID, nil, false)
						}
					} else {
						if ui.Button(c.Key("cmd-"+cmd.ID), label).Disabled(st.busy || !p.Enabled).Clicked() {
							a.savePendingPluginSecrets()
							a.savePluginSettings()
							a.runPluginCommand(p, cmd.ID, nil, false)
						}
					}
				}
				ui.Spacer(c)
				if ui.Button(c.Key("done"), i18n.T("完成")).Clicked() {
					a.savePendingPluginSecrets()
					a.savePluginSettings()
					ss.open = false
				}
			})
		})
	})
}

func (a *App) pluginSettingRow(c *ui.Context, f plugin.SettingField) {
	t := c.Theme()
	ss := &a.pluginsUI.settings
	label := i18n.T(f.Title)
	note := i18n.T(f.Description)

	switch f.Type {
	case "bool":
		ptr := ss.boolBufs[f.ID]
		if ptr == nil {
			b, _ := ss.values[f.ID].(bool)
			ptr = &b
			ss.boolBufs[f.ID] = ptr
		}
		toggleRow(c, "fld-"+f.ID, label, note, ptr, a.savePluginSettings)

	case "segmented":
		ptr := ss.segBufs[f.ID]
		if ptr == nil {
			idx := 0
			ss.segBufs[f.ID] = &idx
			ptr = &idx
		}
		labels := make([]string, len(f.Options))
		for i, o := range f.Options {
			labels[i] = i18n.T(o.Label)
		}
		ui.Row(c.Key("fld-"+f.ID)).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, label)
				if note != "" {
					ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
				}
			})
			before := *ptr
			ui.Segmented(c.Key("seg-"+f.ID), ptr, labels...).Label(label)
			if *ptr != before {
				a.savePluginSettings()
			}
		})

	case "select":
		ptr := ss.strBufs[f.ID]
		if ptr == nil {
			s := ""
			ptr = &s
			ss.strBufs[f.ID] = ptr
		}
		labels := make([]string, len(f.Options))
		for i, o := range f.Options {
			labels[i] = i18n.T(o.Label)
		}
		ui.Row(c.Key("fld-"+f.ID)).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, label)
				if note != "" {
					ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
				}
			})
			curLabel := *ptr
			for _, o := range f.Options {
				if o.Value == *ptr {
					curLabel = i18n.T(o.Label)
					break
				}
			}
			before := curLabel
			ui.Select(c.Key("sel-"+f.ID), &curLabel, labels).Label(label)
			if curLabel != before {
				for _, o := range f.Options {
					if i18n.T(o.Label) == curLabel {
						*ptr = o.Value
						a.savePluginSettings()
						break
					}
				}
			}
		})

	case "string":
		ptr := ss.strBufs[f.ID]
		if ptr == nil {
			s := ""
			ptr = &s
			ss.strBufs[f.ID] = ptr
		}
		ui.Column(c.Key("fld-"+f.ID)).Padding(10, 12).Gap(6).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, label)
				if note != "" {
					ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
				}
			})
			ui.Row(c).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
				before := *ptr
				ui.TextInput(c.Key("in-"+f.ID), ptr).Grow(1).MinWidth(0).
					Placeholder(i18n.T(f.Placeholder)).Label(label)
				if *ptr != before {
					a.savePluginSettings()
				}
				if len(f.Presets) > 0 {
					ui.MenuButton(c.Key("presets-"+f.ID), i18n.T("常用服务"), func(m *ui.Menu) {
						for _, pre := range f.Presets {
							preset := pre
							if m.Item(i18n.T(preset.Name)).Chosen() {
								a.applyPluginPreset(f.ID, preset)
							}
						}
					}).Label(i18n.T("常用服务"))
				}
			})
		})

	case "secret":
		ptr := ss.secretBufs[f.ID]
		if ptr == nil {
			s := ""
			ptr = &s
			ss.secretBufs[f.ID] = ptr
		}
		ui.Column(c.Key("fld-"+f.ID)).Padding(10, 12).Gap(6).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, label)
				if note != "" {
					ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
				}
			})
			ui.Row(c).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
				if ss.secretSaved[f.ID] {
					ui.Text(c, i18n.T("已保存")+" ••••"+ss.secretSuffix[f.ID]).
						TextColor(t.TextMuted).NoWrap().Grow(1)
					if ui.Button(c.Key("remove-"+f.ID), i18n.T("移除")).Label(i18n.T("移除 ") + label).Clicked() {
						a.removePluginSecret(f.ID)
					}
					return
				}
				in := ui.TextInput(c.Key("sec-"+f.ID), ptr).Password().Grow(1).MinWidth(0).
					Placeholder(i18n.T(f.Placeholder)).Label(label)
				if in.Submitted() {
					a.savePluginSecret(f.ID)
				}
				if ui.Button(c.Key("save-"+f.ID), i18n.T("保存")).Label(i18n.T("保存 ") + label).
					Disabled(strings.TrimSpace(*ptr) == "").Clicked() {
					a.savePluginSecret(f.ID)
				}
			})
		})

	case "spaces":
		ui.Column(c.Key("fld-"+f.ID)).Padding(10, 12).Gap(8).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, label)
				if note != "" {
					ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
				}
			})
			for _, sp := range a.state.Spaces {
				if sp.IsExternal() {
					continue
				}
				ptr := ss.spaceBufs[sp.ID]
				if ptr == nil {
					selected := stringSliceValue(ss.values[f.ID])
					on := slices.Contains(selected, sp.ID)
					ptr = &on
					ss.spaceBufs[sp.ID] = ptr
				}
				ui.Row(c.Key("sp-"+sp.ID)).Padding(4, 0).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(24, 24).Radius(6).Center().Background(t.Text.Alpha(0.06)).Children(func() {
						spaceGlyph(c, sp.Icon, sp.Symbol, sp.Name, 13, t.Text)
					})
					ui.Column(c).Grow(1).MinWidth(0).Clip().Gap(1).Children(func() {
						ui.Text(c, sp.Name).FontSize(13).FontWeight(500).SingleLine()
						ui.Text(c, shortPath(sp.Root)).FontSize(11).TextColor(t.TextMuted).SingleLine()
					})
					before := *ptr
					ui.Switch(c.Key("sw-"+sp.ID), ptr).Label(sp.Name)
					if *ptr != before {
						a.savePluginSettings()
					}
				})
			}
		})
	}
}

func (a *App) applyPluginPreset(fieldID string, preset plugin.SettingPreset) {
	ss := &a.pluginsUI.settings
	p := a.findInstalledPlugin(ss.pluginID)
	if p == nil || p.Manifest == nil {
		return
	}
	if len(preset.Values) > 0 {
		for k, v := range preset.Values {
			ss.values[k] = v
			if ptr := ss.strBufs[k]; ptr != nil {
				*ptr = fmt.Sprint(v)
			}
			if ptr := ss.boolBufs[k]; ptr != nil {
				if b, ok := v.(bool); ok {
					*ptr = b
				}
			}
		}
	} else if preset.Value != "" {
		ss.values[fieldID] = preset.Value
		if ptr := ss.strBufs[fieldID]; ptr != nil {
			*ptr = preset.Value
		}
	}
	a.savePluginSettings()
}

func (a *App) pluginMarkdownView(c *ui.Context) {
	sh := &a.pluginsUI.sheet
	if !sh.open {
		return
	}
	w, h := c.Size()
	a.spaceModal(c, "plugin-sheet", &sh.open, func(t *ui.Theme) {
		ui.Column(c.Key("plugin-sheet-body")).Size(min(600, w-t.Space(20)), min(500, h-t.Space(20))).Gap(12).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T(sh.title)).FontSize(16).FontWeight(600).Grow(1)
				if iconButton(c, "close", iconX, i18n.T("关闭"), i18n.T("关闭"), false).Size(28, 28).Clicked() {
					sh.open = false
				}
			})
			ui.Scroll(c.Key("plugin-sheet-scroll")).Grow(1).MinHeight(0).Padding(0, 4, 0, 0).Children(func() {
				markdown.View(c, sh.markdown, &sh.mdOpts)
			})
		})
	})
}

func (a *App) runPluginCommand(p *plugin.Installed, cmdID string, args map[string]any, quiet bool) {
	if p == nil || p.Manifest == nil {
		return
	}
	if p.Manifest.ID == imageHostPluginID && cmdID == "upload-doc-images" {
		a.uploadCurrentDocumentImages(p)
		return
	}
	st := a.pluginRunState(p.Manifest.ID)
	if st.busy {
		return
	}
	inst, err := a.ensurePluginInstance(p)
	if err != nil {
		st.err = err.Error()
		if !quiet {
			a.notify("%v", err)
		}
		return
	}
	if !quiet && cmdID == "sync-now" {
		// Save cleanable edits before a manual sync so local changes go up immediately.
		for _, d := range a.residentDocuments() {
			if d.loadErr == nil && d.dirty() && !d.conflict && !d.missing && !d.ed.Composing() {
				a.withDocument(d, func() { _ = d.save() })
			}
		}
	}

	req := plugin.Request{
		Command: cmdID,
		Args:    args,
	}
	if active := a.state.ActiveSpace(); active != nil && !active.IsExternal() {
		req.ActiveSpace = active.ID
	}
	if a.doc != nil && a.doc.loadErr == nil && slices.Contains(p.Manifest.Permissions, plugin.PermDocumentRead) {
		docText := a.doc.text()
		var sel plugin.Range
		if !a.doc.isWhiteboard() && a.doc.ed != nil {
			s, e := a.doc.ed.Positions()
			if s > e {
				s, e = e, s
			}
			sel = plugin.Range{Start: s, End: e}
		}
		req.Document = &plugin.DocumentContext{
			Path:      a.doc.rel,
			Text:      docText,
			Selection: sel,
		}
	}

	var snapshots []plugin.SpaceSnapshot
	for _, sp := range a.state.Spaces {
		if sp.IsExternal() || sp.Root == "" {
			continue
		}
		snapshots = append(snapshots, plugin.SpaceSnapshot{ID: sp.ID, Name: sp.Name, Root: sp.Root})
	}
	dirty := map[plugin.SpaceFileKey]bool{}
	for _, d := range a.residentDocuments() {
		if d.space == nil || d.space.IsExternal() {
			continue
		}
		if d.dirty() || d.saving || d.conflict || d.missing || (!d.isWhiteboard() && d.ed != nil && d.ed.Composing()) {
			dirty[plugin.SpaceFileKey{SpaceID: d.space.ID, Path: d.rel}] = true
		}
	}

	bin := a.bin
	trashItem := a.trashItem
	trashFn := func(spaceID, spaceName, root, rel string) error {
		kept, addErr := bin.Add(spaceID, spaceName, root, rel, time.Now())
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := trashItem(full); err != nil {
			if addErr == nil {
				_ = bin.Forget(kept)
			}
			return err
		}
		return nil
	}
	spaceSvc := plugin.NewFolderSpaces(snapshots, dirty, trashFn)

	st.busy = true
	st.err = ""

	finish := func(resp *plugin.Response, err error) {
		st.busy = false
		if err != nil {
			st.err = err.Error()
			if !quiet {
				a.notify("插件「%s」执行失败。%v", i18n.T(p.Manifest.Name), err)
			}
			return
		}
		if resp != nil {
			if resp.Status != "" {
				st.status = resp.Status
				if strings.HasPrefix(resp.Status, "连接失败") || strings.HasPrefix(resp.Status, "同步失败") {
					st.err = resp.Status
				}
			}
			if resp.Toast != "" && (!quiet || strings.Contains(resp.Toast, "失败") || strings.Contains(resp.Toast, "冲突") ||
				strings.Contains(resp.Toast, "上传") || strings.Contains(resp.Toast, "下载") || strings.Contains(resp.Toast, "删除")) {
				a.notify("%s", resp.Toast)
			}
			if resp.Markdown != "" {
				a.pluginsUI.sheet = pluginSheetState{
					open:     true,
					title:    i18n.T(p.Manifest.Name),
					markdown: resp.Markdown,
				}
			}
		}
		touched := spaceSvc.Touched()
		if len(touched) > 0 {
			touchedSpaces := map[string]bool{}
			touchedPaths := map[plugin.SpaceFileKey]bool{}
			for _, k := range touched {
				touchedSpaces[k.SpaceID] = true
				touchedPaths[k] = true
				delete(a.pageTrees, k.SpaceID)
			}
			if active := a.state.ActiveSpace(); active != nil && touchedSpaces[active.ID] {
				a.refreshTree()
			}
			a.refreshSearch()
			for _, d := range a.residentDocuments() {
				if touchedPaths[plugin.SpaceFileKey{SpaceID: d.space.ID, Path: d.rel}] {
					a.withDocument(d, a.checkDisk)
				}
			}
		}
		if a.gitChanges.open {
			a.refreshGitChanges()
		}
		if cmdID == "sync-now" && !quiet {
			if conflicts := a.loadPersistedConflicts(); len(conflicts) > 0 && !a.mergeUI.open {
				a.pluginsUI.settings.open = false
				a.gitChanges.open = false
				a.openMergeForConflict(conflicts[0])
			}
		}
	}

	ctx := plugin.WithSpaceService(context.Background(), spaceSvc)
	post := a.asyncPost()
	if a.pluginsUI.sync || post == nil {
		resp, err := inst.Invoke(ctx, req)
		finish(resp, err)
		return
	}
	go func() {
		resp, err := inst.Invoke(ctx, req)
		post(func() { finish(resp, err) })
	}()
}

// onDocumentSaved arms the debounced post-save cloud sync when enabled for spaceID.
func (a *App) onDocumentSaved(spaceID string) {
	if spaceID == "" || spaceID == space.ExternalID {
		return
	}
	p := a.findInstalledPlugin(cloudSyncPluginID)
	if p == nil || !p.Enabled {
		return
	}
	cfg := a.pluginConfig(p)
	if cfg == nil {
		return
	}
	if on, _ := cfg["syncOnSave"].(bool); !on {
		return
	}
	if !slices.Contains(stringSliceValue(cfg["spaces"]), spaceID) {
		return
	}
	a.pluginsUI.saveSyncArm = true
}

// pluginSyncTick drives periodic and post-save cloud sync via c.Now() / c.After().
func (a *App) pluginSyncTick(c *ui.Context) {
	p := a.findInstalledPlugin(cloudSyncPluginID)
	if p == nil || !p.Enabled {
		a.pluginsUI.saveSyncArm = false
		a.pluginsUI.saveSyncDue = time.Time{}
		return
	}
	st := a.pluginRunState(p.Manifest.ID)
	cfg := a.pluginConfig(p)
	if cfg == nil {
		return
	}
	spaces := stringSliceValue(cfg["spaces"])
	if len(spaces) == 0 {
		a.pluginsUI.saveSyncArm = false
		a.pluginsUI.saveSyncDue = time.Time{}
		return
	}
	if a.pluginsUI.saveSyncArm {
		a.pluginsUI.saveSyncArm = false
		a.pluginsUI.saveSyncDue = c.Now().Add(15 * time.Second)
	}
	if st.busy {
		return
	}

	syncOnSave, _ := cfg["syncOnSave"].(bool)
	if !syncOnSave {
		a.pluginsUI.saveSyncDue = time.Time{}
	} else if !a.pluginsUI.saveSyncDue.IsZero() {
		if wait := a.pluginsUI.saveSyncDue.Sub(c.Now()); wait > 0 {
			c.After(wait)
		} else {
			a.pluginsUI.saveSyncDue = time.Time{}
			a.pluginsUI.lastAutoSync = c.Now()
			a.runPluginCommand(p, "sync-now", map[string]any{"trigger": "save"}, true)
			return
		}
	}

	autoSync, _ := cfg["autoSync"].(bool)
	if !autoSync {
		return
	}
	mins := 15
	if raw, ok := cfg["intervalMinutes"]; ok {
		if n, err := strconv.Atoi(fmt.Sprint(raw)); err == nil && n > 0 {
			mins = n
		}
	}
	interval := time.Duration(mins) * time.Minute
	if a.pluginsUI.lastAutoSync.IsZero() {
		a.pluginsUI.lastAutoSync = c.Now()
		c.After(interval)
		return
	}
	if wait := interval - c.Now().Sub(a.pluginsUI.lastAutoSync); wait > 0 {
		c.After(wait)
		return
	}
	a.pluginsUI.lastAutoSync = c.Now()
	a.pluginsUI.saveSyncDue = time.Time{}
	a.runPluginCommand(p, "sync-now", map[string]any{"trigger": "auto"}, true)
}

func whenMatches(cond *plugin.SettingCondition, vals map[string]any) bool {
	if cond == nil {
		return true
	}
	cur := vals[cond.Field]
	if eqBool, ok := cond.Equals.(bool); ok {
		curBool, ok2 := cur.(bool)
		return ok2 && curBool == eqBool
	}
	return fmt.Sprint(cur) == fmt.Sprint(cond.Equals)
}

func stringSliceValue(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
