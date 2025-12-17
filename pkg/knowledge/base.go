// Package knowledge provides a lightweight RAG (Retrieval-Augmented Generation) system
// for the Nightwatch AI Operator. This allows the AI to retrieve relevant runbooks
// and past incident solutions before making diagnosis decisions.
//
// This is a "Smart RAG" implementation using keyword matching instead of vector embeddings,
// proving the RAG concept without the operational complexity of a vector database.
package knowledge

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Runbook represents a stored piece of operational knowledge
type Runbook struct {
	Keywords []string `json:"keywords"`
	Advice   string   `json:"advice"`
	Example  string   `json:"example,omitempty"`
}

// KnowledgeBase stores operational runbooks for RAG retrieval
type KnowledgeBase struct {
	runbooks []Runbook
}

// NewKnowledgeBase creates a new knowledge base from JSON
func NewKnowledgeBase(jsonData string) (*KnowledgeBase, error) {
	var runbooks []Runbook
	if err := json.Unmarshal([]byte(jsonData), &runbooks); err != nil {
		return nil, fmt.Errorf("failed to parse knowledge base: %w", err)
	}
	
	return &KnowledgeBase{runbooks: runbooks}, nil
}

// Search retrieves relevant runbooks based on error text and logs
// This is the "Retrieval" part of RAG
func (kb *KnowledgeBase) Search(errorText, logs string) []Runbook {
	var matches []Runbook
	
	// Combine error and logs for searching
	searchText := strings.ToLower(errorText + " " + logs)
	
	for _, runbook := range kb.runbooks {
		// Check if any keyword matches
		for _, keyword := range runbook.Keywords {
			if strings.Contains(searchText, strings.ToLower(keyword)) {
				matches = append(matches, runbook)
				break // Only add once per runbook
			}
		}
	}
	
	return matches
}

// FormatForPrompt formats retrieved runbooks for LLM prompt augmentation
// This is the "Augmentation" part of RAG
func (kb *KnowledgeBase) FormatForPrompt(runbooks []Runbook) string {
	if len(runbooks) == 0 {
		return "No relevant internal runbooks found."
	}
	
	var formatted strings.Builder
	formatted.WriteString("INTERNAL RUNBOOK KNOWLEDGE:\n")
	
	for i, runbook := range runbooks {
		formatted.WriteString(fmt.Sprintf("\n%d. %s", i+1, runbook.Advice))
		if runbook.Example != "" {
			formatted.WriteString(fmt.Sprintf("\n   Example: %s", runbook.Example))
		}
	}
	
	return formatted.String()
}

// DefaultKnowledgeBase returns a pre-populated knowledge base with common Kubernetes issues
func DefaultKnowledgeBase() *KnowledgeBase {
	jsonData := `[
		{
			"keywords": ["CrashLoopBackOff", "Error", "exit code 1"],
			"advice": "CrashLoopBackOff usually indicates application startup failure. Check: 1) Application logs for errors, 2) Environment variables (especially required ones), 3) ConfigMaps/Secrets mounted correctly, 4) Resource limits (CPU/Memory).",
			"example": "kubectl rollout restart deployment <name> often resolves transient issues"
		},
		{
			"keywords": ["OOMKilled", "memory", "137"],
			"advice": "OOMKilled means the container exceeded memory limits. Solutions: 1) Increase memory limits in deployment spec, 2) Check for memory leaks in application, 3) For Java apps, ensure JVM heap < container limit (use -Xmx flag).",
			"example": "kubectl set resources deployment <name> --limits=memory=1Gi"
		},
		{
			"keywords": ["ImagePullBackOff", "ErrImagePull", "pull"],
			"advice": "Image pull failures indicate registry issues. Check: 1) Image name/tag is correct, 2) Registry credentials (imagePullSecrets), 3) Network connectivity to registry, 4) Image exists in registry.",
			"example": "kubectl create secret docker-registry <name> --docker-server=<registry>"
		},
		{
			"keywords": ["Liveness probe failed", "Readiness probe failed", "probe"],
			"advice": "Probe failures mean health checks are failing. Solutions: 1) Increase initialDelaySeconds if app needs more startup time, 2) Check probe endpoint is correct, 3) Verify app is actually healthy, 4) Adjust timeoutSeconds/periodSeconds.",
			"example": "kubectl set probe deployment <name> --liveness --initial-delay-seconds=60"
		},
		{
			"keywords": ["Insufficient cpu", "Insufficient memory", "resources"],
			"advice": "Resource insufficiency means no nodes have enough capacity. Solutions: 1) Scale down other workloads, 2) Add more nodes to cluster, 3) Reduce resource requests, 4) Enable cluster autoscaling.",
			"example": "kubectl scale deployment <name> --replicas=0 (for non-critical workloads)"
		},
		{
			"keywords": ["FATAL", "APP_MODE", "PROD", "DATABASE_URL", "config"],
			"advice": "Configuration mismatch detected. Common issue: App expects production config but missing required environment variables. Solutions: 1) Set missing env vars, 2) Switch to DEV mode if appropriate, 3) Check ConfigMap/Secret references.",
			"example": "kubectl set env deployment <name> APP_MODE=DEV"
		},
		{
			"keywords": ["Permission denied", "RBAC", "forbidden"],
			"advice": "RBAC/Permission issues indicate missing Kubernetes permissions. Solutions: 1) Check ServiceAccount has required Role/ClusterRole, 2) Verify RoleBinding/ClusterRoleBinding exists, 3) Check namespace matches.",
			"example": "kubectl create rolebinding <name> --role=<role> --serviceaccount=<namespace>:<sa>"
		},
		{
			"keywords": ["Pending", "Unschedulable", "node"],
			"advice": "Pending pods indicate scheduling issues. Check: 1) Node selectors/affinity rules, 2) Taints/tolerations, 3) Resource availability, 4) PVC binding issues.",
			"example": "kubectl describe pod <name> to see scheduling events"
		}
	]`
	
	kb, _ := NewKnowledgeBase(jsonData)
	return kb
}
