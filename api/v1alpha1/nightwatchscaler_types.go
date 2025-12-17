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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NightwatchScalerSpec defines the desired state of NightwatchScaler
type NightwatchScalerSpec struct {
	// TargetDeployment is the name of the deployment to scale
	TargetDeployment string `json:"targetDeployment"`

	// MinReplicas is the lower limit for the number of replicas
	MinReplicas int32 `json:"minReplicas"`

	// MaxReplicas is the upper limit for the number of replicas
	MaxReplicas int32 `json:"maxReplicas"`

	// MetricName is the name of the metric to use for scaling (e.g., cpu, memory, request_count)
	// +optional
	MetricName string `json:"metricName,omitempty"`

	// TargetValue is the target value for the metric
	// +optional
	TargetValue string `json:"targetValue,omitempty"`
}

// NightwatchScalerStatus defines the observed state of NightwatchScaler
type NightwatchScalerStatus struct {
	// CurrentReplicas is the current number of replicas
	CurrentReplicas int32 `json:"currentReplicas"`

	// LastScaleTime is the last time the scaler scaled the deployment
	// +optional
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NightwatchScaler is the Schema for the nightwatchscalers API
type NightwatchScaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NightwatchScalerSpec   `json:"spec,omitempty"`
	Status NightwatchScalerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NightwatchScalerList contains a list of NightwatchScaler
type NightwatchScalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NightwatchScaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NightwatchScaler{}, &NightwatchScalerList{})
}
