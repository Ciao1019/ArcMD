package app

import (
	"testing"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/prefs"
)

func TestQAProtocolPreference(t *testing.T) {
	a, tt := qaApp(t, nil)
	a.prefs.AI = prefs.AI{Provider: "openai", Model: "gpt-6.2-sol"}
	a.openSettings(tabAI)
	tt.Frame()
	if !tt.HasText("当前使用 Responses，流式输出") {
		t.Fatalf("automatic protocol: %q", tt.Texts())
	}
	for _, pick := range []struct{ label, stored, effective string }{
		{"Chat Completions", agent.ProtocolChat, agent.ProtocolChat},
		{"Responses", agent.ProtocolResponses, agent.ProtocolResponses},
		{"自动选择", "", agent.ProtocolResponses},
	} {
		if err := tt.Click(pick.label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saved, err := a.prefStore.Load()
		if err != nil || saved.AI.Protocol != pick.stored || a.qaSettings().EffectiveProtocol() != pick.effective {
			t.Fatalf("%s: saved %+v, settings %+v, err %v", pick.label, saved.AI, a.qaSettings(), err)
		}
	}
	a.prefs.AI.Model = "ordinary-model"
	tt.Frame()
	if !tt.HasText("当前使用 Chat Completions，流式输出") {
		t.Fatalf("model change: %q", tt.Texts())
	}
}
