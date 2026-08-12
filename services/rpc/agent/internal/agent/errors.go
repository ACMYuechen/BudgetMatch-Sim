package agent

import "errors"

// ErrAgentNotFound 表示未找到指定名称的 Agent。
var ErrAgentNotFound = errors.New("agent not found")

// ErrContextTooLarge 表示系统提示词与当前请求已经超过配置的模型上下文上限。
// 历史消息可以裁剪，但当前请求不会被静默截断。
var ErrContextTooLarge = errors.New("agent context exceeds configured token limit")
