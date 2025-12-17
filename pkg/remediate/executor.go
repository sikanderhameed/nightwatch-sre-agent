// Package remediate provides safe command execution for auto-remediation.
// This package handles the actual execution of kubectl commands suggested by the LLM.
package remediate

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Executor handles safe execution of remediation commands.
type Executor struct {
	dryRun         bool
	allowedPrefixes []string
	timeout        time.Duration
}

// NewExecutor creates a new remediation executor.
func NewExecutor(dryRun bool) *Executor {
	return &Executor{
		dryRun:  dryRun,
		timeout: 30 * time.Second,
		allowedPrefixes: []string{
			"kubectl rollout restart",
			"kubectl scale",
			"kubectl set image",
			"kubectl patch",
			"kubectl delete pod",
			"kubectl annotate",
			"kubectl label",
		},
	}
}

// Result represents the result of a remediation attempt.
type Result struct {
	Success bool
	Output  string
	Error   error
	DryRun  bool
}

// Execute runs a kubectl command if it passes safety checks.
func (e *Executor) Execute(ctx context.Context, command string) *Result {
	command = strings.TrimSpace(command)

	// Safety check: Only allow specific command prefixes
	if !e.isAllowed(command) {
		return &Result{
			Success: false,
			Error:   fmt.Errorf("command not in allow-list: %s", command),
		}
	}

	// Dry-run mode: Just report what would be executed
	if e.dryRun {
		return &Result{
			Success: true,
			Output:  fmt.Sprintf("[DRY-RUN] Would execute: %s", command),
			DryRun:  true,
		}
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Execute the command
	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &Result{
			Success: false,
			Output:  string(output),
			Error:   err,
		}
	}

	return &Result{
		Success: true,
		Output:  string(output),
	}
}

// isAllowed checks if a command is in the allow-list.
func (e *Executor) isAllowed(command string) bool {
	for _, prefix := range e.allowedPrefixes {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return false
}

// AddAllowedPrefix adds a new allowed command prefix.
func (e *Executor) AddAllowedPrefix(prefix string) {
	e.allowedPrefixes = append(e.allowedPrefixes, prefix)
}

// SetTimeout sets the command execution timeout.
func (e *Executor) SetTimeout(timeout time.Duration) {
	e.timeout = timeout
}

// IsDryRun returns whether the executor is in dry-run mode.
func (e *Executor) IsDryRun() bool {
	return e.dryRun
}
