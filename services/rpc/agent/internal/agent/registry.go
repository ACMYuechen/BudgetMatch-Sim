package agent

import "fmt"

type Registry struct {
	agents map[string]Agent
}

func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]Agent)}
}

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

func (r *Registry) MustRegister(agent Agent) {
	if err := r.Register(agent); err != nil {
		panic(err)
	}
}

func (r *Registry) Get(name string) (Agent, bool) {
	agent, ok := r.agents[name]
	return agent, ok
}
