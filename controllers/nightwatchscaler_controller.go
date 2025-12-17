/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	// AI-First Imports
	"k8s.io/client-go/kubernetes"
	"nightwatch/pkg/aihealer"

	// IMPORTANT: Update this import path to match your API group/version
	// For now using a placeholder "nightwatch/api/v1alpha1"
	// You might need to change this if your project structure is different
	nightwatchv1alpha1 "nightwatch/api/v1alpha1"
)

// NightwatchScalerReconciler reconciles a NightwatchScaler object
type NightwatchScalerReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	K8sClient kubernetes.Interface // Added for direct API access (logs, events)
}

//+kubebuilder:rbac:groups=nightwatch.io,resources=nightwatchscalers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nightwatch.io,resources=nightwatchscalers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nightwatch.io,resources=nightwatchscalers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete;patch
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NightwatchScalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the NightwatchScaler instance
	scaler := &nightwatchv1alpha1.NightwatchScaler{}
	if err := r.Get(ctx, req.NamespacedName, scaler); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. Fetch the target Deployment
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      scaler.Spec.TargetDeployment,
	}, deployment)

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Target deployment not found", "deployment", scaler.Spec.TargetDeployment)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		
		// 🚨 AI INTERVENTION POINT 1: Deployment fetch error
		// If we can't get the deployment, maybe the name is wrong or it was deleted?
		// We could use AI here, but let's stick to the scaling loop for now.
		return ctrl.Result{}, err
	}

	// 3. Check for specific failure conditions (e.g., replicas = 0 but desired > 0, or old generation)
	if isDeploymentUnhealthy(deployment) {
		
		// 🚨 AI INTERVENTION POINT 2: Unhealthy Deployment detected!
		logger.Info("⚠️ Detected unhealthy deployment, activating AI Healer")

		// Initialize AI Healer
		healer := aihealer.NewAIHealer(r.K8sClient, aihealer.DefaultConfig())

		// Ask AI to fix it
		result := healer.HandleScalingFailure(ctx, aihealer.HealingOptions{
			Namespace:       req.Namespace,
			DeploymentName:  deployment.Name,
			ScalerName:      scaler.Name,
			ErrorMessage:    getDeploymentError(deployment),
			CurrentReplicas: deployment.Status.Replicas,
			DesiredReplicas: *deployment.Spec.Replicas,
		})

		if result.AutoHealed {
			logger.Info("✅ AI Auto-Remediation successful!")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	} else {
		// 4. NEW: Check for SILENT FAILURES (Prometheus 500 errors)
		// Only check if deployment looks healthy to Kubernetes
		
		healer := aihealer.NewAIHealer(r.K8sClient, aihealer.DefaultConfig())
		isSilentFailure, msg := healer.CheckSilentFailure(ctx, req.Namespace, scaler.Spec.TargetDeployment)
		
		if isSilentFailure {
			// 🚨 AI INTERVENTION POINT 3: Silent Failure Detected!
			logger.Info("⚠️ Silent Failure Detected (High Error Rate)", "msg", msg)

			result := healer.HandleSilentFailure(ctx, aihealer.HealingOptions{
				Namespace:       req.Namespace,
				DeploymentName:  deployment.Name,
				ScalerName:      scaler.Name,
				ErrorMessage:    msg, // "High Error Rate Detected: 5.2%"
				CurrentReplicas: deployment.Status.Replicas,
				DesiredReplicas: *deployment.Spec.Replicas,
			})
			
			if result.AutoHealed {
				logger.Info("✅ AI Auto-Remediation of Silent Failure successful!")
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
		}
	}

	// ... (Rest of standard scaling logic would go here) ...
	
	// Reconcile more frequently (1m) to catch Prometheus alerts quickly
	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NightwatchScalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nightwatchv1alpha1.NightwatchScaler{}).
		Complete(r)
}

// Helper functions (simplified for brevity)
func isDeploymentUnhealthy(d *appsv1.Deployment) bool {
	// Example check: Available replicas < Desired replicas for > 5 min
	if d.Status.AvailableReplicas < *d.Spec.Replicas {
		return true
	}
	// Check for conditions
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentReplicaFailure && c.Status == "True" {
			return true
		}
	}
	return false
}

func getDeploymentError(d *appsv1.Deployment) string {
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentReplicaFailure && c.Status == "True" {
			return fmt.Sprintf("[%s] %s", c.Reason, c.Message)
		}
	}
	return "Deployment is unhealthy (Available < Desired)"
}
