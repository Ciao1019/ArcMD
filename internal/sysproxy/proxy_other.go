//go:build !darwin && !windows

package sysproxy

func loadOSConfig() Config {
	return Config{}
}
