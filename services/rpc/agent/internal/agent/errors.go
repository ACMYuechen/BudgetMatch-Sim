package agent

import "errors"

// ErrAgentNotFound 表示未找到指定名称的 Agent。
var ErrAgentNotFound = errors.New("agent not found")

// ErrContextTooLarge 表示系统提示词与当前请求已经超过配置的模型上下文上限。
// 历史消息可以裁剪，但当前请求不会被静默截断。
var ErrContextTooLarge = errors.New("agent context exceeds configured token limit")

// ErrTurnConflict 表示同一会话中的 turn_id 已绑定到不同的请求内容。
// 同内容网络重试可安全重放，不同内容必须生成新的 turn_id。
var ErrTurnConflict = errors.New("agent turn id is already bound to a different request")

// ErrInvalidInput 表示推荐请求未满足 Agent 业务入口的参数边界。
var ErrInvalidInput = errors.New("invalid agent recommendation input")
