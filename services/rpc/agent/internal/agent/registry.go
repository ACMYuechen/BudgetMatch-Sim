package agent

import "fmt"

// Registry 是 Agent 实例的注册表，按名称管理多个 Agent 实现。
type Registry struct {
	agents map[string]Agent // agents 保存名称到 Agent 实例的映射
}

// NewRegistry 创建一个空的 Agent 注册表。
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]Agent)}
}

// Register 将 Agent 注册到注册表中。若 Agent 为 nil 或名称为空则返回错误。
func (r *Registry) Register(agent Agent) error {
	if agent == nil {
		return fmt.Errorf("agent is nil")
	}
	name := agent.Name()
	if name == "" {
		return fmt.Errorf("agent name is empty")
	}
	r.agents[name] = agent
	return nil
}

// MustRegister 与 Register 相同，但在出错时会 panic。
func (r *Registry) MustRegister(agent Agent) {
	if err := r.Register(agent); err != nil {
		panic(err)
	}
}

// Get 根据名称获取已注册的 Agent。
func (r *Registry) Get(name string) (Agent, bool) {
	agent, ok := r.agents[name]
	return agent, ok
}
