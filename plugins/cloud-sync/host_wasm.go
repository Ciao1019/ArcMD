//go:build wasip1 && wasm

package main

import (
	"errors"
	"runtime"
)

//go:wasmimport arcmd call
//go:noescape
func arcmdCall(reqPtr *byte, reqLen uint32) uint32

//go:wasmimport arcmd read_result
//go:noescape
func arcmdReadResult(dstPtr *byte, dstLen uint32) uint32

func exchangeHostFrame(reqFrame []byte) ([]byte, error) {
	if len(reqFrame) == 0 {
		return nil, errors.New("empty host request frame")
	}
	n := arcmdCall(&reqFrame[0], uint32(len(reqFrame)))
	runtime.KeepAlive(reqFrame)
	if n == 0 {
		return nil, errors.New("host call failed")
	}
	out := make([]byte, n)
	got := arcmdReadResult(&out[0], n)
	runtime.KeepAlive(out)
	if got != n {
		return nil, errors.New("short host result read")
	}
	return out, nil
}
