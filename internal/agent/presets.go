package agent

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
)

// Preset is a model service the settings offer by name, its address
// filled in. The user still chooses the model, as services rename theirs
// often.
type Preset struct {
	ID, Name string
	BaseURL  string
	// Local services need no API key.
	Local bool
	// Example is a model name to show as the field's placeholder.
	Example string
}

// Presets are the services offered, then Custom for any other speaking
// the Chat Completions format.
var Presets = []Preset{
	{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1"},
	{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com", Example: "deepseek-v4-pro"},
	{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"},
	{ID: "ollama", Name: "Ollama", BaseURL: "http://localhost:11434/v1", Local: true},
	{ID: Custom, Name: "自定义"},
}

// Custom is the preset of a service the user gives the address of.
const Custom = "custom"

// PresetOf is the preset with the ID, Custom's when there is none.
func PresetOf(id string) Preset {
	for _, p := range Presets {
		if p.ID == id {
			return p
		}
	}
	return Presets[len(Presets)-1]
}

// Settings are what is needed to ask a model.
type Settings struct {
	BaseURL, Model, APIKey string
	Protocol               string
}

// Errors of settings that cannot be used.
var (
	ErrNoModel    = errors.New("no model is set")
	ErrNoKey      = errors.New("no API key is set")
	ErrBadAddress = errors.New("the address must start with https:// (http:// only for this computer)")
)

// IsLocal reports an address on this computer, which may use plain http
// and needs no key.
func IsLocal(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Check reports why the settings cannot be used, or nil. Documents go to
// the service, so only https leaves this computer.
func (s Settings) Check() error {
	if err := s.CheckService(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Model) == "" {
		return ErrNoModel
	}
	return nil
}

// CheckService reports why the service cannot be reached with the settings,
// whatever the model: enough to list its models.
func (s Settings) CheckService() error {
	u, err := url.Parse(strings.TrimSpace(s.BaseURL))
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && IsLocal(s.BaseURL))) {
		return ErrBadAddress
	}
	if strings.TrimSpace(s.APIKey) == "" && !IsLocal(s.BaseURL) {
		return ErrNoKey
	}
	return nil
}

// An empty protocol is automatic, including for preferences saved before the picker.
const (
	ProtocolChat      = "chat-completions"
	ProtocolResponses = "responses"
)

func responsesReasoner(model string) bool {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "openai/") {
	case "gpt-6.2-sol", "gpt-6.2-astra":
		return true
	}
	return false
}

func (s Settings) EffectiveProtocol() string {
	switch s.Protocol {
	case ProtocolChat, ProtocolResponses:
		return s.Protocol
	}
	u, err := url.Parse(strings.TrimSpace(s.BaseURL))
	if err == nil && u.Scheme == "https" && responsesReasoner(s.Model) && (strings.EqualFold(u.Hostname(), "api.openai.com") || strings.EqualFold(u.Hostname(), "openrouter.ai")) {
		return ProtocolResponses
	}
	return ProtocolChat
}

type ModelService interface {
	Provider
	Models(context.Context) ([]string, error)
}

func (s Settings) Provider() ModelService {
	u, _ := url.Parse(strings.TrimSpace(s.BaseURL))
	replay := u != nil && (u.Hostname() == "api.deepseek.com" || strings.HasSuffix(u.Hostname(), ".deepseek.com"))
	chat := ChatCompletions{BaseURL: strings.TrimSpace(s.BaseURL), Model: strings.TrimSpace(s.Model), APIKey: strings.TrimSpace(s.APIKey), ReplayReasoning: replay}
	if s.EffectiveProtocol() == ProtocolResponses {
		return &Responses{chat}
	}
	return &chat
}
