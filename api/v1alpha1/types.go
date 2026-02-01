package v1alpha1

// GroupVersion identifies the API group and version for Fiso CRDs.
const (
	Group   = "fiso.io"
	Version = "v1alpha1"
)

// FlowDefinition represents a Fiso inbound event pipeline as a Kubernetes CRD.
type FlowDefinition struct {
	TypeMeta   `json:",inline"`
	ObjectMeta `json:"metadata,omitempty"`
	Spec       FlowDefinitionSpec   `json:"spec"`
	Status     FlowDefinitionStatus `json:"status,omitempty"`
}

// FlowDefinitionSpec defines the desired state of a FlowDefinition.
type FlowDefinitionSpec struct {
	Source        SourceSpec        `json:"source"`
	Transform     *TransformSpec    `json:"transform,omitempty"`
	Sink          SinkSpec          `json:"sink"`
	ErrorHandling ErrorHandlingSpec `json:"errorHandling,omitempty"`
}

// SourceSpec defines the event source.
type SourceSpec struct {
	Type   string            `json:"type"`
	Config map[string]string `json:"config,omitempty"`
}

// TransformSpec defines the event transform.
type TransformSpec struct {
	CEL string `json:"cel"`
}

// SinkSpec defines the event sink.
type SinkSpec struct {
	Type   string            `json:"type"`
	Config map[string]string `json:"config,omitempty"`
}

// ErrorHandlingSpec defines error handling behavior.
type ErrorHandlingSpec struct {
	DeadLetterTopic string `json:"deadLetterTopic,omitempty"`
	MaxRetries      int    `json:"maxRetries,omitempty"`
}

// FlowDefinitionStatus defines the observed state of a FlowDefinition.
type FlowDefinitionStatus struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// LinkTarget represents a Fiso-Link outbound target as a Kubernetes CRD.
type LinkTarget struct {
	TypeMeta   `json:",inline"`
	ObjectMeta `json:"metadata,omitempty"`
	Spec       LinkTargetSpec   `json:"spec"`
	Status     LinkTargetStatus `json:"status,omitempty"`
}

// LinkTargetSpec defines the desired state of a LinkTarget.
type LinkTargetSpec struct {
	Protocol       string              `json:"protocol"`
	Host           string              `json:"host"`
	Auth           *LinkAuthSpec       `json:"auth,omitempty"`
	CircuitBreaker *CircuitBreakerSpec `json:"circuitBreaker,omitempty"`
	Retry          *RetrySpec          `json:"retry,omitempty"`
	AllowedPaths   []string            `json:"allowedPaths,omitempty"`
}

// LinkAuthSpec defines authentication for a link target.
type LinkAuthSpec struct {
	Type       string `json:"type"` // Bearer, APIKey, Basic, Vault
	SecretName string `json:"secretName,omitempty"`
	VaultPath  string `json:"vaultPath,omitempty"`
}

// CircuitBreakerSpec defines circuit breaker settings.
type CircuitBreakerSpec struct {
	FailureThreshold int    `json:"failureThreshold,omitempty"`
	ResetTimeout     string `json:"resetTimeout,omitempty"`
}

// RetrySpec defines retry settings.
type RetrySpec struct {
	MaxAttempts int    `json:"maxAttempts,omitempty"`
	Backoff     string `json:"backoff,omitempty"`
}

// LinkTargetStatus defines the observed state of a LinkTarget.
type LinkTargetStatus struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// TypeMeta describes an individual object kind and API version.
// Mirrors metav1.TypeMeta without importing k8s dependencies.
type TypeMeta struct {
	Kind       string `json:"kind,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
}

// ObjectMeta is metadata attached to every resource.
// Mirrors metav1.ObjectMeta (subset) without importing k8s dependencies.
type ObjectMeta struct {
	Name        string            `json:"name,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Generation  int64             `json:"generation,omitempty"`
}

// FlowDefinitionList contains a list of FlowDefinitions.
type FlowDefinitionList struct {
	TypeMeta `json:",inline"`
	Items    []FlowDefinition `json:"items"`
}

// LinkTargetList contains a list of LinkTargets.
type LinkTargetList struct {
	TypeMeta `json:",inline"`
	Items    []LinkTarget `json:"items"`
}
