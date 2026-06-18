package mcp

import "time"

type Config struct {
	Enabled bool     `json:"enabled,optional"`
	Command string   `json:"command,optional"`
	Args    []string `json:"args,optional"`
	Timeout int64    `json:"timeout,optional"`
}

func (c Config) RequestTimeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Second
	}
	return time.Duration(c.Timeout) * time.Millisecond
}

func (c Config) Ready() bool {
	return c.Enabled && c.Command != ""
}
