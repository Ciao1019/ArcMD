// Package agent answers questions about a space's documents the way a
// coding agent explores a repository: the model is given tools to list,
// search and read the documents (package spacefs), calls them as it sees
// fit, step after step, and answers citing the lines it read. There is no
// embedding or vector index; recall comes from the model choosing what to
// search for and reading on from what it finds.
//
// The package holds a provider-neutral conversation (Message), the loop
// that runs a model's tool calls (Agent.Run), and adapters to the wire
// formats of model services (ChatCompletions). It does not depend on the
// window: events go out through a callback the app forwards to the main
// thread.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Role is who said a message.
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
	// ToolRole carries one tool call's result back to the model.
	ToolRole Role = "tool"
)

// ToolCall is a model's request to run a tool, Args its JSON arguments.
type ToolCall struct {
	ID   string
	Name string
	Args string
}

// Message is one turn of a conversation, in no provider's format.
type Message struct {
	Role Role
	Text string
	// Reasoning is what a thinking model sent as its reasoning, kept for
	// providers that want it back with the calls it led to.
	Reasoning string
	// responses retains ordered wire items for stateless Responses tool loops.
	responses *responseReplay
	// Calls are an assistant message's tool calls.
	Calls []ToolCall
	// CallID is the call a tool message answers.
	CallID string
	// IsError marks a tool message whose call failed.
	IsError bool
}

// ToolSpec describes a tool to the model; Parameters is a JSON schema.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// Request is one step's input to a model.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolSpec
}

// Delta is a piece of a streamed reply.
type Delta struct {
	Text, Reasoning string
}

// Usage is what a step cost, when the provider says.
type Usage struct {
	Input, Output int
}

// Response is a step's complete reply.
type Response struct {
	Message Message
	Usage   Usage
}

// Provider is a model service. Complete streams the reply's text through
// onDelta as it comes and returns the whole reply; it stops when ctx is
// done.
type Provider interface {
	Complete(ctx context.Context, req Request, onDelta func(Delta)) (Response, error)
}

// Tool is a tool the model may call. Run returns the text the model reads;
// an error is told to the model as the result, so it can try again.
type Tool struct {
	Spec ToolSpec
	Run  func(ctx context.Context, args json.RawMessage) (string, error)
}

// EventKind is what happened during a run.
type EventKind int

const (
	// TextDelta and ReasoningDelta are streamed text of the reply.
	TextDelta EventKind = iota
	ReasoningDelta
	// ToolStart and ToolDone frame one tool call.
	ToolStart
	ToolDone
	// StepDone ends a model step; Message is the step's message.
	StepDone
	// Retrying reports a failed request about to be sent again.
	Retrying
)

// Event is something the user may be shown while the agent works.
type Event struct {
	Kind    EventKind
	Text    string
	Call    ToolCall
	Result  string
	IsError bool
	Message Message
	Usage   Usage
}

// Defaults of an Agent's budgets.
const (
	DefaultMaxSteps = 16
	// DefaultContextChars is how much text the conversation may hold
	// before older tool results are dropped from it; about 100k tokens of
	// Chinese, within every current model's window.
	DefaultContextChars = 120_000
	maxToolResult       = 30_000
)

// Agent runs a conversation's next turn.
type Agent struct {
	Provider Provider
	System   string
	Tools    []Tool
	// MaxSteps is how many model steps one turn may take, tool calls
	// between them; the last step is asked to answer without tools.
	MaxSteps int
	// ContextChars bounds the conversation sent each step.
	ContextChars int
	// Retries is how many times a failed request is sent again when the
	// service says it may be (rate limits, overload, a dropped stream).
	Retries int
	// Wait waits before a retry; tests replace it so they never sleep.
	Wait func(ctx context.Context, attempt int) error
}

// Run continues the conversation (history ends with the user's message)
// until the model answers without calling tools, and returns the messages
// it added. Events are delivered one at a time, in order. A cancelled ctx
// stops it with ctx's error and the messages added so far.
func (a *Agent) Run(ctx context.Context, history []Message, emit func(Event)) ([]Message, error) {
	if emit == nil {
		emit = func(Event) {}
	}
	var mu sync.Mutex
	send := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		emit(e)
	}
	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}
	specs := make([]ToolSpec, len(a.Tools))
	byName := map[string]Tool{}
	for i, t := range a.Tools {
		specs[i] = t.Spec
		byName[t.Spec.Name] = t
	}
	var added []Message
	for step := 1; ; step++ {
		req := Request{System: a.System, Messages: a.fit(append(append([]Message(nil), history...), added...)), Tools: specs}
		if step >= maxSteps {
			// Out of steps: answer with what was found.
			req.Tools = nil
			req.Messages = append(req.Messages, Message{Role: User, Text: "[You have used all your tool calls for this question. Answer now from what you have read, and say what you could not check.]"})
		}
		resp, err := a.complete(ctx, req, send)
		if err != nil {
			return added, err
		}
		msg := resp.Message
		msg.Role = Assistant
		added = append(added, msg)
		send(Event{Kind: StepDone, Message: msg, Usage: resp.Usage})
		if len(msg.Calls) == 0 {
			return added, nil
		}
		results := make([]Message, len(msg.Calls))
		var wg sync.WaitGroup
		for i, call := range msg.Calls {
			wg.Add(1)
			go func() {
				defer wg.Done()
				send(Event{Kind: ToolStart, Call: call})
				out, err := runTool(ctx, byName, call)
				res := Message{Role: ToolRole, CallID: call.ID, Text: out}
				if err != nil {
					res.Text, res.IsError = "Error: "+err.Error(), true
				}
				results[i] = res
				send(Event{Kind: ToolDone, Call: call, Result: res.Text, IsError: res.IsError})
			}()
		}
		wg.Wait()
		added = append(added, results...)
		if err := ctx.Err(); err != nil {
			return added, err
		}
	}
}

func runTool(ctx context.Context, tools map[string]Tool, call ToolCall) (string, error) {
	t, ok := tools[call.Name]
	if !ok {
		return "", fmt.Errorf("there is no tool named %q", call.Name)
	}
	args := json.RawMessage(strings.TrimSpace(call.Args))
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if !json.Valid(args) {
		return "", fmt.Errorf("the arguments are not valid JSON: %s", call.Args)
	}
	out, err := t.Run(ctx, args)
	if err != nil {
		return "", err
	}
	if len(out) > maxToolResult {
		out = out[:maxToolResult] + "\n(result cut; ask for less)"
	}
	return out, nil
}

// complete sends one step, again when it fails in a way worth retrying
// and nothing of the reply was shown yet.
func (a *Agent) complete(ctx context.Context, req Request, send func(Event)) (Response, error) {
	for attempt := 0; ; attempt++ {
		streamed := false
		resp, err := a.Provider.Complete(ctx, req, func(d Delta) {
			if d.Reasoning != "" {
				streamed = true
				send(Event{Kind: ReasoningDelta, Text: d.Reasoning})
			}
			if d.Text != "" {
				streamed = true
				send(Event{Kind: TextDelta, Text: d.Text})
			}
		})
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		if streamed || attempt >= a.Retries || !Retryable(err) {
			return Response{}, err
		}
		send(Event{Kind: Retrying, Text: err.Error()})
		wait := a.Wait
		if wait == nil {
			wait = backoff
		}
		if err := wait(ctx, attempt); err != nil {
			return Response{}, err
		}
	}
}

func backoff(ctx context.Context, attempt int) error {
	select {
	case <-time.After(time.Duration(1<<attempt) * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fit budgets the history as the active provider will see it. Retain the
// pending tool batch even when that batch alone exceeds the soft budget.
func (a *Agent) fit(msgs []Message) []Message {
	budget := a.ContextChars
	if budget <= 0 {
		budget = DefaultContextChars
	}
	responses, _ := a.Provider.(*Responses)
	chat, _ := a.Provider.(*ChatCompletions)
	plainSize := func(m Message) int {
		size := len(m.Text)
		if chat != nil && chat.ReplayReasoning {
			size += len(m.Reasoning)
		}
		for _, call := range m.Calls {
			size += len(call.Args) + len(call.Name) + len(call.ID)
		}
		return size
	}
	size := len(a.System)
	protected := len(msgs)
	for i, m := range msgs {
		switch m.Role {
		case User, Assistant:
			protected = len(msgs)
			if m.Role == Assistant && len(m.Calls) > 0 {
				protected = i
			}
		}
		if m.responses.matches(responses) {
			for _, item := range m.responses.items {
				size += len(item)
			}
		} else {
			size += plainSize(m)
		}
	}
	for i := 0; i < protected && size > budget; i++ {
		m := &msgs[i]
		if m.responses.matches(responses) {
			for _, item := range m.responses.items {
				size -= len(item)
			}
			m.responses = nil
			size += plainSize(*m)
		}
	}
	for i := 0; i < protected && size > budget; i++ {
		if msgs[i].Role == ToolRole && len(msgs[i].Text) > len(omitted) {
			size -= len(msgs[i].Text) - len(omitted)
			msgs[i].Text = omitted
		}
	}
	return msgs
}

const omitted = "[Earlier result omitted to save space; call the tool again if you need it.]"

// APIError is a model service's refusal.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("the model service answered %d", e.Status)
	}
	return fmt.Sprintf("the model service answered %d: %s", e.Status, e.Message)
}

// ErrStreamCut is a reply whose stream ended before it was complete.
var ErrStreamCut = errors.New("the reply stopped before it was complete")

// Retryable reports an error worth sending the request again for: rate
// limits, the service's own failures, and a dropped stream.
func Retryable(err error) bool {
	var api *APIError
	if errors.As(err, &api) {
		return api.Status == 429 || api.Status == 408 || api.Status >= 500
	}
	return errors.Is(err, ErrStreamCut)
}
