package jsx

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moejs"
)

// Options bounds an embedded compilation; there is no external compiler path.
type Options struct{ Timeout time.Duration }

//go:embed assets/mdx-compiler.js
var compilerJS string
var compilerOnce sync.Once
var compilerModule *moejs.Module
var compilerError error

const MaxSourceBytes = 512 << 10
const CompilerMemoryLimit = 256 << 20

// Compile runs only the trusted, embedded MDX compiler inside moejs. Document
// expressions are parsed into output code, NEVER executed in this runtime.
// There are no host bindings, importer, Node modules, filesystem or web APIs.
func Compile(source string, opt Options) (Manifest, error) {
	var m Manifest
	if len(source) > MaxSourceBytes {
		return m, fmt.Errorf("MDX source exceeds %d bytes", MaxSourceBytes)
	}
	compilerOnce.Do(func() { compilerModule, compilerError = moejs.Compile("embedded-mdx-compiler.js", compilerJS) })
	if compilerError != nil {
		return m, fmt.Errorf("moejs compiler initialization: %w", compilerError)
	}
	limit := opt.Timeout
	if limit <= 0 || limit > 5*time.Second {
		limit = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: CompilerMemoryLimit, DisableDynamicCode: true})
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { rt.Interrupt(ctx.Err()); close(interrupted) })
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	if err := rt.Load(compilerModule); err != nil {
		return m, fmt.Errorf("moejs load: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return m, err
	}
	var nonce [16]byte
	for {
		if _, err := rand.Read(nonce[:]); err != nil {
			return m, err
		}
		if !strings.Contains(source, hex.EncodeToString(nonce[:])) {
			break
		}
	}
	input, _ := json.Marshal(map[string]string{"source": source, "namespace": hex.EncodeToString(nonce[:])})
	hook, err := compilerModule.Hook("compileMDX")
	if err != nil {
		return m, err
	}
	result, err := rt.Call(hook, moejs.String(string(input)))
	if err != nil {
		return m, fmt.Errorf("moejs MDX: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return m, err
	}
	if err = rt.UnmarshalWith(result, &m, moejs.DecodeOptions{MaxBytes: 16 << 20, MaxNodes: 200000}); err != nil {
		return m, fmt.Errorf("moejs result: %w", err)
	}
	m.SourceSHA256 = SourceSHA256(source)
	for i := range m.Blocks {
		b := &m.Blocks[i]
		if b.Error == "" && b.Compiled != "" {
			b.HTML = islandPage(b.Source, b.Compiled)
		}
		b.Compiled = ""
	}
	return m, nil
}
