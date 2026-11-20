package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// settingsAI is the model the questions go to, in the order it is set
// up: the service's address (typed, or filled from a preset), its API key,
// kept in the local credential store, then the model, chosen from the
// list the service gives when asked, or typed.
func (a *App) settingsAI(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, i18n.T("AI 问答"), fmt.Sprintf(i18n.T("在问答面板（%s）里向当前空间提问。模型会搜索、阅读空间里的文档后回答并标出出处，不建立向量索引。"), a.keyLabel("ask")))
	ai := &a.prefs.AI
	if ai.BaseURL == "" && ai.Provider != agent.Custom {
		ai.BaseURL = aiPreset(ai.Provider).BaseURL
	}
	a.dropStaleModels()
	sep := func() { ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06)) }
	row := func(key, label, note string, control func()) {
		ui.Column(c.Key(key)).Padding(10, 12).Gap(6).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, label)
				if note != "" {
					ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
				}
			})
			ui.Row(c).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(control)
		})
	}
	settingsGroup(c, "ai", func() {
		row("address", "Base URL", i18n.T("模型服务的 API 根地址"), func() {
			before := ai.BaseURL
			ui.TextInput(c.Key("field"), &ai.BaseURL).Grow(1).MinWidth(0).Placeholder("https://api.example.com/v1").Label(i18n.T("服务地址"))
			if ai.BaseURL != before {
				ai.Provider = agent.Custom
				for _, p := range agent.Presets {
					if p.BaseURL != "" && strings.TrimSpace(ai.BaseURL) == p.BaseURL {
						ai.Provider = p.ID
					}
				}
				a.savePrefs()
			}
			ui.MenuButton(c.Key("presets"), i18n.T("常用服务"), func(m *ui.Menu) {
				for _, p := range agent.Presets {
					if p.BaseURL == "" {
						continue
					}
					if m.Item(p.Name).Checked(ai.Provider == p.ID).Chosen() {
						ai.Provider, ai.BaseURL = p.ID, p.BaseURL
						a.savePrefs()
					}
				}
			}).Label(i18n.T("常用服务"))
		})
		sep()
		settings := a.qaSettings()
		local := agent.IsLocal(settings.BaseURL)
		row("key", "API Key", i18n.T("保存在本机配置目录"), func() {
			switch {
			case a.qa.key != "":
				ui.Text(c, i18n.T("已保存")+" ••••"+lastRunes(a.qa.key, 4)).TextColor(t.TextMuted).NoWrap().Grow(1)
				if ui.Button(c.Key("remove"), i18n.T("移除")).Clicked() {
					a.removeAIKey()
				}
			default:
				placeholder := i18n.T("粘贴 API Key")
				if local {
					placeholder = i18n.T("本机服务可以不填")
				}
				in := ui.TextInput(c.Key("field"), &a.qa.keyInput).Password().Grow(1).MinWidth(0).
					Placeholder(placeholder).Label(i18n.T("粘贴 API Key"))
				if in.Submitted() {
					a.saveAIKey()
				}
				if ui.Button(c.Key("save"), i18n.T("保存")).Disabled(strings.TrimSpace(a.qa.keyInput) == "").Clicked() {
					a.saveAIKey()
				}
			}
		})
		sep()
		row("model", i18n.T("模型名称"), a.modelsNote(), func() {
			before := ai.Model
			if len(a.qa.models) > 0 && !a.qa.manualModel {
				options := a.qa.models
				if ai.Model != "" && !slices.Contains(options, ai.Model) {
					options = append([]string{ai.Model}, options...)
				}
				ui.Combobox(c.Key("choice"), &ai.Model, options).Grow(1).MinWidth(0).
					Placeholder(i18n.T("选择模型")).Label(i18n.T("选择模型"))
			} else {
				placeholder := aiPreset(ai.Provider).Example
				if placeholder == "" {
					placeholder = i18n.T("模型名称")
				}
				ui.TextInput(c.Key("field"), &ai.Model).Grow(1).MinWidth(0).Placeholder(placeholder).Label(i18n.T("输入模型名称"))
			}
			if ai.Model != before {
				a.savePrefs()
			}
			if len(a.qa.models) > 0 {
				label := i18n.T("手动输入")
				if a.qa.manualModel {
					label = i18n.T("从列表选择")
				}
				if ui.Button(c.Key("manual"), label).Clicked() {
					a.qa.manualModel = !a.qa.manualModel
				}
			}
			label := i18n.T("获取模型")
			if a.qa.modelsBusy {
				label = i18n.T("获取中…")
			}
			if ui.Button(c.Key("fetch"), label).Disabled(a.qa.modelsBusy || a.modelListSettings().CheckService() != nil).
				Tooltip(i18n.T("从服务拉取可用的模型列表")).Clicked() {
				a.fetchModels()
			}
		})
		sep()
		protocols := []string{"", agent.ProtocolChat, agent.ProtocolResponses}
		names := []string{i18n.T("自动选择"), "Chat Completions", "Responses"}
		effective := names[slices.Index(protocols, settings.EffectiveProtocol())]
		row("protocol", i18n.T("接口协议"), fmt.Sprintf(i18n.T("当前使用 %s，流式输出"), effective), func() {
			at := max(0, slices.Index(protocols, ai.Protocol))
			before := at
			ui.Segmented(c.Key("choice"), &at, names...).Label(i18n.T("接口协议"))
			if at != before {
				ai.Protocol = protocols[at]
				a.savePrefs()
			}
		})
	})
	status := i18n.T("可以提问了。")
	color := t.TextMuted
	if err := a.qaReady(); err != nil {
		status, color = err.Error(), t.Danger
	}
	ui.Text(c.Key("ai-status"), status).FontSize(12).TextColor(color)
	if err := a.qa.modelsErr; err != nil {
		ui.Text(c.Key("models-error"), i18n.T("获取模型列表失败：")+qaErrorText(err)).FontSize(12).TextColor(t.TextMuted).Selectable()
	}
	ui.Text(c, i18n.T("提问时，问题和模型读到的文档片段会发送给上面的服务；服务地址须为 https（本机服务除外）。ArcMD 不会把文档发到别处。")).
		FontSize(12).TextColor(t.TextMuted)
}

// lastRunes are the last n characters of s, to tell keys apart.
func lastRunes(s string, n int) string {
	r := []rune(s)
	return string(r[max(0, len(r)-n):])
}

// modelsNote says how the model list stands, under the row's label.
func (a *App) modelsNote() string {
	q := &a.qa
	switch {
	case q.modelsBusy:
		return i18n.T("正在获取模型列表…")
	case q.modelsErr != nil:
		return i18n.T("没有拿到列表，可手动输入")
	case len(q.models) > 0:
		return fmt.Sprintf(i18n.T("共 %d 个可用模型"), len(q.models))
	case q.modelsFor != "":
		return i18n.T("服务没有列出模型，请手动输入")
	case a.modelListSettings().CheckService() != nil:
		return i18n.T("先填写 Base URL 和 API Key，再获取模型")
	}
	return i18n.T("点「获取模型」选择，或直接输入")
}
