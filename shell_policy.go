package core

import "strings"

// ShellRoute describes how a shell-backed tool should execute.
type ShellRoute int

const (
	// ShellRouteSandboxFirst runs inside the sandbox and may fall back later.
	ShellRouteSandboxFirst ShellRoute = iota
	// ShellRouteHostDirect skips the sandbox and should ask for approval first.
	ShellRouteHostDirect
)

// ShellDecision is the policy result for a shell-backed tool call.
type ShellDecision struct {
	Route  ShellRoute
	Reason string
}

// ShellRequest captures the information needed to route shell-backed tools.
type ShellRequest struct {
	ToolName string
	Command  string
	Safety   string
}

// ShellPolicy decides whether a shell-backed tool should run sandboxed first
// or directly on the host.
type ShellPolicy func(ShellRequest) ShellDecision

// ShellRule matches a request and optionally returns a routing decision.
type ShellRule func(ShellRequest) (ShellDecision, bool)

// RuleBasedShellPolicy builds a ShellPolicy from ordered rules.
func RuleBasedShellPolicy(rules ...ShellRule) ShellPolicy {
	return func(req ShellRequest) ShellDecision {
		for _, rule := range rules {
			if decision, ok := rule(req); ok {
				return decision
			}
		}
		return ShellDecision{Route: ShellRouteSandboxFirst}
	}
}

// DefaultShellPolicy returns the default routing policy for shell-backed tools.
func DefaultShellPolicy() ShellPolicy {
	return RuleBasedShellPolicy(
		shellSafetyRouteRule("network", "safety marked as network"),
		shellSafetyRouteRule("privileged", "safety marked as privileged"),
		packageManagerRouteRule(),
		gitNetworkRouteRule(),
		networkTransferRouteRule(),
	)
}

// DecideShellRequest applies the configured policy or the default one.
func DecideShellRequest(policy ShellPolicy, req ShellRequest) ShellDecision {
	if policy == nil {
		policy = DefaultShellPolicy()
	}
	return policy(req)
}

// ShellRequestFromToolArgs builds a ShellRequest from a tool call.
func ShellRequestFromToolArgs(toolName string, args map[string]any) ShellRequest {
	req := ShellRequest{ToolName: toolName}
	req.Safety, _ = args["safety"].(string)

	switch toolName {
	case "run_shell":
		req.Command, _ = args["command"].(string)
	case "python_runtime":
		code, _ := args["code"].(string)
		req.Command = pythonCommand(code)
	}

	return req
}

func shellSafetyRouteRule(safety, reason string) ShellRule {
	return func(req ShellRequest) (ShellDecision, bool) {
		if strings.EqualFold(strings.TrimSpace(req.Safety), safety) {
			return ShellDecision{Route: ShellRouteHostDirect, Reason: reason}, true
		}
		return ShellDecision{}, false
	}
}
// Updates TUI
func packageManagerRouteRule() ShellRule {
	prefixes := []string{
		"npm install", "npm update", "npm add", "npm view", "npm publish", "npm login", "npm logout",
		"pnpm install", "pnpm update", "pnpm add", "pnpm dlx",
		"yarn install", "yarn add", "yarn up", "yarn global add",
		"bun install", "bun add",
		"pip install", "pip3 install",
		"python -m pip install", "python3 -m pip install",
		"uv pip install",
		"go get", "go install", "go mod download", "go mod tidy",
		"cargo add", "cargo install", "cargo update",
		"brew install", "brew upgrade", "brew update",
	}
	return shellPrefixRouteRule(prefixes, "known package-manager command")
}

func gitNetworkRouteRule() ShellRule {
	prefixes := []string{
		"git clone", "git fetch", "git pull", "git push",
	}
	return shellPrefixRouteRule(prefixes, "known git network command")
}

func networkTransferRouteRule() ShellRule {
	prefixes := []string{
		"curl ", "wget ",
	}
	return shellPrefixRouteRule(prefixes, "known network transfer command")
}

func shellPrefixRouteRule(prefixes []string, reason string) ShellRule {
	return func(req ShellRequest) (ShellDecision, bool) {
		cmd := strings.ToLower(strings.TrimSpace(req.Command))
		for _, prefix := range prefixes {
			if strings.HasPrefix(cmd, prefix) {
				return ShellDecision{Route: ShellRouteHostDirect, Reason: reason}, true
			}
		}
		return ShellDecision{}, false
	}
}
