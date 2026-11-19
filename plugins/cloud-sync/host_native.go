//go:build !(wasip1 && wasm)

package main

import (
	"errors"
	"sync"
)

var (
	nativeBridgeMu sync.RWMutex
	nativeBridge   func([]byte) ([]byte, error)
)

func setNativeHostBridge(fn func([]byte) ([]byte, error)) {
	nativeBridgeMu.Lock()
	nativeBridge = fn
	nativeBridgeMu.Unlock()
}

func exchangeHostFrame(reqFrame []byte) ([]byte, error) {
	nativeBridgeMu.RLock()
	fn := nativeBridge
	nativeBridgeMu.RUnlock()
	if fn == nil {
		return nil, errors.New("host bridge is not attached")
	}
	return fn(reqFrame)
}
