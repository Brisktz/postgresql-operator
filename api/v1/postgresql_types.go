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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PostgresqlSpec defines the desired state of Postgresql.
type PostgresqlSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// PostgresVersion postgresql version
	PostgresVersion string `json:"postgresVersion"`

	// PostgresqlPassword postgresql password
	PostgresqlPassword string `json:"postgresqlPassword"`
}

// PostgresqlStatus defines the observed state of Postgresql.
type PostgresqlStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Conditions []metav1.Condition `json:"conditions"`

	// StartTime 定义了资源的创建时间
	StartTime *metav1.Time `json:"startTime,omitempty"`
}

// postgresql type
const (
	PostgresqlSecretReady          string = "PostgresqlSecretReady"
	PostgresqlServiceHeadlessReady string = "PostgresqlServiceHeadless"
	PostgresqlServiceReady         string = "PostgresqlServiceReady"
	PostgresqlStatefulSetReady     string = "PostgresqlStatefulSetReady"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Postgresql is the Schema for the postgresqls API.
type Postgresql struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostgresqlSpec   `json:"spec,omitempty"`
	Status PostgresqlStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PostgresqlList contains a list of Postgresql.
type PostgresqlList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Postgresql `json:"items"`
}

// init() 只会被执行一次
func init() {
	SchemeBuilder.Register(&Postgresql{}, &PostgresqlList{})
}
