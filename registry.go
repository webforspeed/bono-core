package core

// Registry holds tool definitions. Instance-based so each agent (or server session) can have its own set.
type Registry struct {
	tools map[string]*ToolDef
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]*ToolDef)}
}

// Register adds a tool to the registry, replacing any existing tool with the same name.
func (r *Registry) Register(t *ToolDef) {
	r.tools[t.Name] = t
}

// Get returns the tool definition for a given name.
func (r *Registry) Get(name string) (*ToolDef, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Tools returns API-ready []Tool for sending to the LLM.
// If names is empty, returns all registered tools.
// If names is provided, returns only matching tools (unknown names are skipped).
func (r *Registry) Tools(names ...string) []Tool {
	if len(names) == 0 {
		tools := make([]Tool, 0, len(r.tools))
		for _, t := range r.tools {
			tools = append(tools, t.Tool())
		}
		return tools
	}
	tools := make([]Tool, 0, len(names))
	for _, name := range names {
		if t, ok := r.tools[name]; ok {
			tools = append(tools, t.Tool())
		}
	}
	return tools
}
