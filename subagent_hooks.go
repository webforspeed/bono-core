package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts input to a URL-safe slug: lowercase, non-alphanumeric replaced
// with hyphens, consecutive hyphens collapsed, leading/trailing hyphens stripped,
// truncated to maxLen.
func slugify(input string, maxLen int) string {
	s := strings.ToLower(input)
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > maxLen {
		s = s[:maxLen]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// persistHook writes the subagent's final content to a file under the resolved
// directory template. On revision (Meta["output_path"] already set), it
// overwrites the existing file.
type persistHook struct {
	dirTemplate string
}

// PersistHook returns a SubAgentHook that writes the subagent's content to
// <dirTemplate>/<timestamp>-<slug>.md. The template supports {cwd} expansion.
// ~ is resolved to the user's home directory.
func PersistHook(dirTemplate string) SubAgentHook {
	return &persistHook{dirTemplate: dirTemplate}
}

func (h *persistHook) AfterComplete(_ context.Context, result *SubAgentResult) error {
	outputPath := result.Meta["output_path"]

	if outputPath == "" {
		dir := h.resolveDir(result.CWD)
		slug := slugify(result.Input, 50)
		if slug == "" {
			slug = "plan"
		}
		filename := fmt.Sprintf("%d-%s.md", time.Now().Unix(), slug)
		outputPath = filepath.Join(dir, filename)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("persist hook: mkdir: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(result.Content), 0644); err != nil {
		return fmt.Errorf("persist hook: write: %w", err)
	}

	result.Meta["output_path"] = outputPath
	return nil
}

func (h *persistHook) resolveDir(cwd string) string {
	dir := h.dirTemplate

	// Expand ~ to home directory.
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}

	// Expand {cwd} placeholder.
	if strings.Contains(dir, "{cwd}") {
		// Strip leading slash so path nests cleanly under the base dir.
		cwdClean := strings.TrimPrefix(cwd, "/")
		dir = strings.ReplaceAll(dir, "{cwd}", cwdClean)
	}

	return dir
}

// approvalHook prompts the user to approve, reject, or revise the subagent output.
type approvalHook struct {
	getCallback func() func(SubAgentResult) SubAgentApprovalResponse
}

// ApprovalHook returns a SubAgentHook that calls Agent.OnSubAgentApproval.
// Pass the agent's callback getter so the hook evaluates it at call time.
func ApprovalHook(getCallback func() func(SubAgentResult) SubAgentApprovalResponse) SubAgentHook {
	return &approvalHook{getCallback: getCallback}
}

func (h *approvalHook) AfterComplete(_ context.Context, result *SubAgentResult) error {
	cb := h.getCallback()
	if cb == nil {
		result.Meta["approval"] = "approved"
		return nil
	}

	resp := cb(*result)
	switch resp.Action {
	case SubAgentApprove:
		result.Meta["approval"] = "approved"
	case SubAgentReject:
		result.Meta["approval"] = "rejected"
	case SubAgentRevise:
		result.Meta["approval"] = "revise"
		result.Meta["feedback"] = resp.Feedback
	}
	return nil
}
