package jsx

import (
	"context"
	"errors"
	"github.com/MoeForge/moejs"
	"strings"
	"testing"
	"time"
)

func TestMoeArbitraryInputAndNoExpressionExecution(t *testing.T) {
	for _, source := range []string{
		"# 新文件 😀\n\n<Counter initial={17+24} label=\"新内容\" />\n",
		"# Nested\n\n<Callout title=\"Nested\">\n\n**bold**\n\n<Counter initial={8} />\n\n</Callout>\n",
		"<Counter initial={(()=>{throw new Error('MUST NOT EXECUTE IN COMPILER')})()} />\n",
	} {
		m, e := Compile(source, Options{})
		if e != nil || !m.HasHTML() {
			t.Fatalf("%v %+v", e, m)
		}
		if RestoreSource(m.Markdown, m.Namespace) != source {
			t.Fatal("source altered")
		}
		if strings.Contains(m.Blocks[0].HTML, "unsafe-eval") {
			t.Fatal("weakened CSP")
		}
	}
}
func TestMoeRejectsModuleImportAndOversize(t *testing.T) {
	m, e := Compile("<Callout>{import('node:fs')}</Callout>\n", Options{})
	if e != nil || len(m.Blocks) != 1 || m.Blocks[0].Error == "" || m.HasHTML() {
		t.Fatalf("import allowed: %v %+v", e, m)
	}
	if _, e = Compile(strings.Repeat("a", MaxSourceBytes+1), Options{}); e == nil {
		t.Fatal("missing size bound")
	}
	if _, e = Compile("<Counter />", Options{Timeout: time.Nanosecond}); e == nil {
		t.Fatal("missing deadline")
	}
}
func TestMoeRuntimeGuards(t *testing.T) {
	m, e := moejs.Compile("guard.js", `export function spin(){while(true){}}; export function allocate(){const x=[];for(;;)x.push('x'.repeat(65536))};export function capabilities(){return [typeof process,typeof require,typeof fetch,typeof document]}`)
	if e != nil {
		t.Fatal(e)
	}
	r := moejs.NewRuntime(moejs.Options{MemoryLimit: 1 << 20, DisableDynamicCode: true})
	if e = r.Load(m); e != nil {
		t.Fatal(e)
	}
	caps, _ := m.Hook("capabilities")
	v, e := r.Call(caps)
	var got []string
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Unmarshal(v, &got); e != nil {
		t.Fatal(e)
	}
	for _, x := range got {
		if x != "undefined" {
			t.Fatalf("unexpected host capability %v", got)
		}
	}
	alloc, _ := m.Hook("allocate")
	if _, e = r.Call(alloc); !errors.Is(e, moejs.ErrMemoryLimit) {
		t.Fatalf("memory bound: %v", e)
	}
	r = moejs.NewRuntime(moejs.Options{MemoryLimit: CompilerMemoryLimit, DisableDynamicCode: true})
	if e = r.Load(m); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { r.Interrupt(ctx.Err()); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
	}()
	spin, _ := m.Hook("spin")
	if _, e = r.Call(spin); e == nil {
		t.Fatal("spin not interrupted")
	}
}
