//go:build windows

package sysproxy

import "golang.org/x/sys/windows/registry"

func loadOSConfig() Config {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return Config{}
	}
	defer k.Close()

	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return Config{}
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil || server == "" {
		return Config{}
	}
	override, _, _ := k.GetStringValue("ProxyOverride")
	return parseWindowsProxy(server, override)
}
