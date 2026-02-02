package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// SchemeGroupVersion is the GroupVersion for fiso CRDs.
	SchemeGroupVersion = schema.GroupVersion{Group: Group, Version: Version}

	// SchemeBuilder builds the scheme for fiso CRDs.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds fiso CRD types to a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&FlowDefinitionCR{},
		&FlowDefinitionCRList{},
		&LinkTargetCR{},
		&LinkTargetCRList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

// Resource returns a GroupResource for the given resource name.
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// FlowDefinitionCR is the controller-runtime compatible FlowDefinition CRD.
type FlowDefinitionCR struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              FlowDefinitionSpec   `json:"spec"`
	Status            FlowDefinitionStatus `json:"status,omitempty"`
}

// FlowDefinitionCRList contains a list of FlowDefinitionCR.
type FlowDefinitionCRList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FlowDefinitionCR `json:"items"`
}

// LinkTargetCR is the controller-runtime compatible LinkTarget CRD.
type LinkTargetCR struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              LinkTargetSpec   `json:"spec"`
	Status            LinkTargetStatus `json:"status,omitempty"`
}

// LinkTargetCRList contains a list of LinkTargetCR.
type LinkTargetCRList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LinkTargetCR `json:"items"`
}
