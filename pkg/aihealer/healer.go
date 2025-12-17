// Package aihealer provides AI-powered self-healing capabilities for the Nightwatch Operator.
// This is the integration layer that combines LLM diagnosis, notifications, and remediation.
//
// THE AI-FIRST DIFFERENCE:
// Traditional operators: Error → Log → Alert → Human fixes
// AI-First operators:    Error → LLM diagnoses → Auto-remediate → Notify
package aihealer

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nightwatch/pkg/knowledge"
	"nightwatch/pkg/llm"
	"nightwatch/pkg/notify"
	"nightwatch/pkg/observability"
	"nightwatch/pkg/remediate"
)

// AIHealer is the brain of the AI-First Nightwatch Operator.
// It diagnoses issues using LLM and auto-remediates when safe.
type AIHealer struct {
	llmClient    *llm.Client
	promClient   *observability.PrometheusClient
	lokiClient   *observability.LokiClient
	executor     *remediate.Executor
	k8sClient    kubernetes.Interface
	knowledgeBase *knowledge.KnowledgeBase // RAG: Runbook knowledge base
	
	// Configuration
	autoExecute         bool
	confidenceThreshold float64
}

// Config holds configuration for the AIHealer.
type Config struct {
	AutoExecute         bool
	ConfidenceThreshold float64
	DryRun              bool
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	autoExec, _ := strconv.ParseBool(os.Getenv("AUTO_EXECUTE"))
	threshold, err := strconv.ParseFloat(os.Getenv("CONFIDENCE_THRESHOLD"), 64)
	if err != nil {
		threshold = 0.85
	}
	dryRun, _ := strconv.ParseBool(os.Getenv("DRY_RUN"))

	return Config{
		AutoExecute:         autoExec,
		ConfidenceThreshold: threshold,
		DryRun:              dryRun,
	}
}

// NewAIHealer creates a new AI-powered healer.
func NewAIHealer(k8sClient kubernetes.Interface, cfg Config) *AIHealer {
	// Initialize observability clients (best effort)
	promClient, _ := observability.NewPrometheusClient()
	lokiClient, _ := observability.NewLokiClient()

	return &AIHealer{
		llmClient:           llm.NewClient(),
		promClient:          promClient,
		lokiClient:          lokiClient,
		executor:            remediate.NewExecutor(cfg.DryRun),
		k8sClient:           k8sClient,
		knowledgeBase:       knowledge.DefaultKnowledgeBase(), // RAG: Initialize with default runbooks
		autoExecute:         cfg.AutoExecute,
		confidenceThreshold: cfg.ConfidenceThreshold,
	}
}

// HealingResult represents the outcome of a healing attempt.
type HealingResult struct {
	Diagnosed    bool
	AutoHealed   bool
	Diagnosis    *llm.Diagnosis
	Error        error
	ActionTaken  string
}

// CheckSilentFailure checks Prometheus for high error rates
func (h *AIHealer) CheckSilentFailure(ctx context.Context, namespace, deploymentName string) (bool, string) {
	if h.promClient == nil {
		return false, ""
	}
	isHigh, msg, err := h.promClient.IsHighErrorRate(ctx, namespace, deploymentName)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to check silent failure")
		return false, ""
	}
	return isHigh, msg
}

// HandleSilentFailure handles alerts from Prometheus (5xx errors)
func (h *AIHealer) HandleSilentFailure(ctx context.Context, opts HealingOptions) *HealingResult {
	logger := log.FromContext(ctx)
	result := &HealingResult{}

	logger.Info("📉 Silent Failure Detected (High 5xx Rate)", 
		"namespace", opts.Namespace, 
		"deployment", opts.DeploymentName)

	// 1. GATHER CONTEXT (Events + LOKI Logs)
	events, _ := h.getRecentEvents(ctx, opts.Namespace, opts.DeploymentName)
	
	var logs string
	if h.lokiClient != nil {
		lLogs, err := h.lokiClient.FetchLogs(opts.Namespace, opts.DeploymentName)
		if err != nil {
			logger.Error(err, "Failed to fetch Loki logs")
			logs = "Loki fetch failed: " + err.Error()
		} else {
			logs = lLogs
		}
	} else {
		logs = "Loki client not configured"
	}

	// 2. DIAGNOSE
	diagnosis, err := h.llmClient.DiagnoseScalingFailure(ctx, llm.DiagnoseOptions{
		Namespace:       opts.Namespace,
		DeploymentName:  opts.DeploymentName,
		Events:          events,
		PodStatus:       fmt.Sprintf("Current: %d, Desired: %d", opts.CurrentReplicas, opts.DesiredReplicas),
		ReplicaSetInfo:  "Alert Source: Prometheus High Error Rate (Silent Failure)",
		Logs:            logs,
	})

	if err != nil {
		logger.Error(err, "LLM diagnosis failed")
		result.Error = err
		return result
	}
	
	result.Diagnosed = true
	result.Diagnosis = diagnosis

	// 3. DECIDE & ACT
	if h.shouldAutoHeal(diagnosis) {
		result.ActionTaken = "auto-remediation"
		h.executeAutoRemediation(ctx, opts, diagnosis, result)
	} else {
		result.ActionTaken = "notification"
		h.notifyHuman(ctx, opts, diagnosis)
	}

	return result
}

// HandleScalingFailure is called when Nightwatch detects a scaling issue.
// This is the main entry point for AI-powered diagnosis and remediation.
func (h *AIHealer) HandleScalingFailure(ctx context.Context, opts HealingOptions) *HealingResult {
	logger := log.FromContext(ctx)
	result := &HealingResult{}

	logger.Info("🤖 AI Healer activated",
		"namespace", opts.Namespace,
		"deployment", opts.DeploymentName,
		"error", opts.ErrorMessage)

	// 1. GATHER CONTEXT
	events, err := h.getRecentEvents(ctx, opts.Namespace, opts.DeploymentName)
	if err != nil {
		logger.Error(err, "Failed to get events")
		events = "Could not retrieve events"
	}

	podLogs, err := h.getPodLogs(ctx, opts.Namespace, opts.DeploymentName)
	if err != nil {
		logger.Error(err, "Failed to get pod logs")
		podLogs = "Could not retrieve logs"
	}
	
	// RAG: Retrieve relevant knowledge from runbook database
	var knowledgeContext string
	if h.knowledgeBase != nil {
		runbooks := h.knowledgeBase.Search(opts.ErrorMessage, podLogs)
		if len(runbooks) > 0 {
			knowledgeContext = h.knowledgeBase.FormatForPrompt(runbooks)
			logger.Info("📚 Retrieved relevant runbooks", "count", len(runbooks))
		}
	}

	// 2. CONSULT THE LLM (The AI-First Magic)
	diagnosis, err := h.llmClient.DiagnoseScalingFailure(ctx, llm.DiagnoseOptions{
		Namespace:        opts.Namespace,
		DeploymentName:   opts.DeploymentName,
		Events:           events,
		PodStatus:        fmt.Sprintf("Current: %d, Desired: %d - Error: %s", opts.CurrentReplicas, opts.DesiredReplicas, opts.ErrorMessage),
		ReplicaSetInfo:   fmt.Sprintf("Scaler: %s", opts.ScalerName),
		Logs:             podLogs,
		KnowledgeContext: knowledgeContext, // RAG: Inject retrieved knowledge
	})

	if err != nil {
		logger.Error(err, "LLM diagnosis failed")
		result.Error = err
		return result
	}

	result.Diagnosed = true
	result.Diagnosis = diagnosis

	logger.Info("🧠 AI Diagnosis complete",
		"rootCause", diagnosis.RootCause,
		"severity", diagnosis.Severity,
		"confidence", diagnosis.Confidence,
		"canAutoFix", diagnosis.CanAutoFix)

	// 3. DECIDE: Auto-heal or notify human
	if h.shouldAutoHeal(diagnosis) {
		result.ActionTaken = "auto-remediation"
		h.executeAutoRemediation(ctx, opts, diagnosis, result)
	} else {
		result.ActionTaken = "notification"
		h.notifyHuman(ctx, opts, diagnosis)
	}

	return result
}

// HealingOptions contains context for the healing operation.
type HealingOptions struct {
	Namespace       string
	DeploymentName  string
	ScalerName      string
	ErrorMessage    string
	CurrentReplicas int32
	DesiredReplicas int32
}

func (h *AIHealer) shouldAutoHeal(diagnosis *llm.Diagnosis) bool {
	if !h.autoExecute {
		return false
	}
	if diagnosis.FixCommand == "" {
		return false
	}
	if !diagnosis.CanAutoFix {
		return false
	}
	if diagnosis.Confidence < h.confidenceThreshold {
		return false
	}
	if diagnosis.Severity == "high" || diagnosis.Severity == "critical" {
		return false
	}
	return true
}

func (h *AIHealer) executeAutoRemediation(ctx context.Context, opts HealingOptions, diagnosis *llm.Diagnosis, result *HealingResult) {
	logger := log.FromContext(ctx)

	logger.Info("🔧 Executing auto-remediation",
		"command", diagnosis.FixCommand,
		"confidence", diagnosis.Confidence)

	execResult := h.executor.Execute(ctx, diagnosis.FixCommand)

	// Send Slack Alert
	notify.SendAlert(
		fmt.Sprintf("Auto-Remediation: %s", opts.DeploymentName),
		diagnosis.RootCause,
		diagnosis.FixCommand,
		execResult.Success,
	)

	if execResult.Success {
		result.AutoHealed = true
		logger.Info("✅ Auto-remediation successful", "output", execResult.Output)
	} else {
		logger.Error(execResult.Error, "❌ Auto-remediation failed", "output", execResult.Output)
	}
}

func (h *AIHealer) notifyHuman(ctx context.Context, opts HealingOptions, diagnosis *llm.Diagnosis) {
	logger := log.FromContext(ctx)

	logger.Info("📣 Notifying human (auto-heal not triggered)",
		"reason", h.getNoAutoHealReason(diagnosis))

	// Send Slack Alert
	notify.SendAlert(
		fmt.Sprintf("Incident Alert: %s", opts.DeploymentName),
		fmt.Sprintf("Root Cause: %s\nReason for no auto-fix: %s", diagnosis.RootCause, h.getNoAutoHealReason(diagnosis)),
		diagnosis.FixCommand, // might be empty
		false,
	)
}

func (h *AIHealer) getNoAutoHealReason(diagnosis *llm.Diagnosis) string {
	if !h.autoExecute {
		return "auto-execute disabled"
	}
	if diagnosis.FixCommand == "" {
		return "no fix command suggested"
	}
	if !diagnosis.CanAutoFix {
		return "LLM marked as not auto-fixable"
	}
	if diagnosis.Confidence < h.confidenceThreshold {
		return fmt.Sprintf("confidence %.2f below threshold %.2f", diagnosis.Confidence, h.confidenceThreshold)
	}
	if diagnosis.Severity == "high" || diagnosis.Severity == "critical" {
		return "severity too high"
	}
	return "unknown"
}

// getRecentEvents fetches recent Kubernetes events for a deployment.
func (h *AIHealer) getRecentEvents(ctx context.Context, namespace, deploymentName string) (string, error) {
	events, err := h.k8sClient.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", deploymentName),
	})
	if err != nil {
		return "", err
	}

	var eventStrings []string
	for _, event := range events.Items {
		eventStrings = append(eventStrings, fmt.Sprintf("[%s] %s: %s",
			event.Type, event.Reason, event.Message))
	}

	if len(eventStrings) == 0 {
		return "No recent events", nil
	}

	// Return last 20 events
	if len(eventStrings) > 20 {
		eventStrings = eventStrings[len(eventStrings)-20:]
	}

	return strings.Join(eventStrings, "\n"), nil
}

// getPodLogs fetches recent logs from pods of a deployment.
func (h *AIHealer) getPodLogs(ctx context.Context, namespace, deploymentName string) (string, error) {
	// List pods with deployment label
	pods, err := h.k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", deploymentName),
	})
	if err != nil {
		return "", err
	}

	if len(pods.Items) == 0 {
		return "No pods found", nil
	}

	// Get logs from first pod
	pod := pods.Items[0]
	tailLines := int64(50)

	logs, err := h.k8sClient.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		TailLines: &tailLines,
	}).Do(ctx).Raw()

	if err != nil {
		return "", err
	}

	return string(logs), nil
}
