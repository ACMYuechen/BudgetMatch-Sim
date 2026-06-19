package agent

import "errors"

// ErrAgentNotFound 表示未找到指定名称的 Agent。
var ErrAgentNotFound = errors.New("agent not found")
