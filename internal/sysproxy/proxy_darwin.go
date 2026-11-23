//go:build darwin

package sysproxy

import (
	"context"
	"os/exec"
	"time"
)

func loadOSConfig() Config {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return Config{}
	}
	return parseScutilProxy(string(out))
}
