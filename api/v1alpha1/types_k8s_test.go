package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSchemeRegistration(t *testing.T) {
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}

	// Verify FlowDefinitionCR is registered
	gvk := SchemeGroupVersion.WithKind("FlowDefinitionCR")
	obj, err := s.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New for FlowDefinitionCR: %v", err)
	}
	if _, ok := obj.(*FlowDefinitionCR); !ok {
		t.Errorf("expected *FlowDefinitionCR, got %T", obj)
	}

	// Verify LinkTargetCR is registered
	gvk = SchemeGroupVersion.WithKind("LinkTargetCR")
	obj, err = s.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New for LinkTargetCR: %v", err)
	}
	if _, ok := obj.(*LinkTargetCR); !ok {
		t.Errorf("expected *LinkTargetCR, got %T", obj)
	}

	// Verify list types
	gvk = SchemeGroupVersion.WithKind("FlowDefinitionCRList")
	obj, err = s.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New for FlowDefinitionCRList: %v", err)
	}
	if _, ok := obj.(*FlowDefinitionCRList); !ok {
		t.Errorf("expected *FlowDefinitionCRList, got %T", obj)
	}

	gvk = SchemeGroupVersion.WithKind("LinkTargetCRList")
	obj, err = s.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New for LinkTargetCRList: %v", err)
	}
	if _, ok := obj.(*LinkTargetCRList); !ok {
		t.Errorf("expected *LinkTargetCRList, got %T", obj)
	}
}

func TestFlowDefinitionCR_DeepCopy(t *testing.T) {
	original := &FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels:    map[string]string{"app": "fiso"},
		},
		Spec: FlowDefinitionSpec{
			Source: SourceSpec{
				Type:   "kafka",
				Config: map[string]string{"brokers": "kafka:9092"},
			},
			Transform:     &TransformSpec{CEL: "message.value"},
			Sink:          SinkSpec{Type: "http", Config: map[string]string{"url": "http://svc"}},
			ErrorHandling: ErrorHandlingSpec{MaxRetries: 3, DeadLetterTopic: "dlq"},
		},
		Status: FlowDefinitionStatus{Phase: "Ready", Message: "ok"},
	}

	copied := original.DeepCopy()

	// Verify independence
	copied.Labels["app"] = "changed"
	if original.Labels["app"] != "fiso" {
		t.Error("deep copy labels not independent")
	}

	copied.Spec.Source.Config["brokers"] = "changed"
	if original.Spec.Source.Config["brokers"] != "kafka:9092" {
		t.Error("deep copy source config not independent")
	}

	copied.Spec.Sink.Config["url"] = "changed"
	if original.Spec.Sink.Config["url"] != "http://svc" {
		t.Error("deep copy sink config not independent")
	}

	// Verify nil produces nil
	var nilFD *FlowDefinitionCR
	if nilFD.DeepCopy() != nil {
		t.Error("DeepCopy of nil should return nil")
	}
}

func TestFlowDefinitionCR_DeepCopyObject(t *testing.T) {
	fd := &FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: FlowDefinitionSpec{
			Source: SourceSpec{Type: "kafka"},
			Sink:   SinkSpec{Type: "http"},
		},
	}

	obj := fd.DeepCopyObject()
	if _, ok := obj.(*FlowDefinitionCR); !ok {
		t.Errorf("expected *FlowDefinitionCR, got %T", obj)
	}
}

func TestLinkTargetCR_DeepCopy(t *testing.T) {
	original := &LinkTargetCR{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "crm",
			Namespace: "default",
			Labels:    map[string]string{"tier": "external"},
		},
		Spec: LinkTargetSpec{
			Protocol:       "https",
			Host:           "crm.example.com",
			Auth:           &LinkAuthSpec{Type: "Bearer", SecretName: "crm-secret"},
			CircuitBreaker: &CircuitBreakerSpec{FailureThreshold: 5, ResetTimeout: "30s"},
			Retry:          &RetrySpec{MaxAttempts: 3, Backoff: "exponential"},
			AllowedPaths:   []string{"/api/v2/**"},
		},
		Status: LinkTargetStatus{Phase: "Ready"},
	}

	copied := original.DeepCopy()

	// Verify independence
	copied.Spec.AllowedPaths[0] = "changed"
	if original.Spec.AllowedPaths[0] != "/api/v2/**" {
		t.Error("deep copy allowed paths not independent")
	}

	copied.Spec.Auth.SecretName = "changed"
	if original.Spec.Auth.SecretName != "crm-secret" {
		t.Error("deep copy auth not independent")
	}

	copied.Spec.CircuitBreaker.FailureThreshold = 99
	if original.Spec.CircuitBreaker.FailureThreshold != 5 {
		t.Error("deep copy circuit breaker not independent")
	}

	copied.Spec.Retry.MaxAttempts = 99
	if original.Spec.Retry.MaxAttempts != 3 {
		t.Error("deep copy retry not independent")
	}

	// Verify nil
	var nilLT *LinkTargetCR
	if nilLT.DeepCopy() != nil {
		t.Error("DeepCopy of nil should return nil")
	}
}

func TestLinkTargetCR_DeepCopyObject(t *testing.T) {
	lt := &LinkTargetCR{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: LinkTargetSpec{
			Protocol: "https",
			Host:     "example.com",
		},
	}

	obj := lt.DeepCopyObject()
	if _, ok := obj.(*LinkTargetCR); !ok {
		t.Errorf("expected *LinkTargetCR, got %T", obj)
	}
}

func TestFlowDefinitionCRList_DeepCopy(t *testing.T) {
	list := &FlowDefinitionCRList{
		Items: []FlowDefinitionCR{
			{ObjectMeta: metav1.ObjectMeta{Name: "a"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "b"}},
		},
	}

	copied := list.DeepCopy()
	if len(copied.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(copied.Items))
	}

	copied.Items[0].Name = "changed"
	if list.Items[0].Name != "a" {
		t.Error("deep copy list items not independent")
	}

	var nilList *FlowDefinitionCRList
	if nilList.DeepCopy() != nil {
		t.Error("DeepCopy of nil list should return nil")
	}
}

func TestLinkTargetCRList_DeepCopy(t *testing.T) {
	list := &LinkTargetCRList{
		Items: []LinkTargetCR{
			{ObjectMeta: metav1.ObjectMeta{Name: "a"}},
		},
	}

	copied := list.DeepCopy()
	copied.Items[0].Name = "changed"
	if list.Items[0].Name != "a" {
		t.Error("deep copy list items not independent")
	}

	var nilList *LinkTargetCRList
	if nilList.DeepCopy() != nil {
		t.Error("DeepCopy of nil list should return nil")
	}
}

func TestConversion_FlowDefinitionCR_ToFlowDefinition(t *testing.T) {
	cr := &FlowDefinitionCR{
		TypeMeta: metav1.TypeMeta{Kind: "FlowDefinition", APIVersion: "fiso.io/v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-flow",
			Namespace:   "production",
			Labels:      map[string]string{"env": "prod"},
			Annotations: map[string]string{"note": "test"},
			Generation:  3,
		},
		Spec: FlowDefinitionSpec{
			Source: SourceSpec{Type: "kafka"},
			Sink:   SinkSpec{Type: "temporal"},
		},
		Status: FlowDefinitionStatus{Phase: "Ready", Message: "ok"},
	}

	fd := cr.ToFlowDefinition()

	if fd.Kind != "FlowDefinition" {
		t.Errorf("kind mismatch: %s", fd.Kind)
	}
	if fd.Name != "my-flow" {
		t.Errorf("name mismatch: %s", fd.Name)
	}
	if fd.Namespace != "production" {
		t.Errorf("namespace mismatch: %s", fd.Namespace)
	}
	if fd.Labels["env"] != "prod" {
		t.Error("labels not converted")
	}
	if fd.Annotations["note"] != "test" {
		t.Error("annotations not converted")
	}
	if fd.Generation != 3 {
		t.Error("generation not converted")
	}
	if fd.Spec.Source.Type != "kafka" {
		t.Error("spec not converted")
	}
	if fd.Status.Phase != "Ready" {
		t.Error("status not converted")
	}
}

func TestConversion_LinkTargetCR_ToLinkTarget(t *testing.T) {
	cr := &LinkTargetCR{
		TypeMeta: metav1.TypeMeta{Kind: "LinkTarget", APIVersion: "fiso.io/v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "crm",
			Namespace:  "default",
			Generation: 2,
		},
		Spec: LinkTargetSpec{
			Protocol: "https",
			Host:     "crm.example.com",
		},
		Status: LinkTargetStatus{Phase: "Ready", Message: "validated"},
	}

	lt := cr.ToLinkTarget()

	if lt.Kind != "LinkTarget" {
		t.Errorf("kind mismatch: %s", lt.Kind)
	}
	if lt.Name != "crm" {
		t.Errorf("name mismatch: %s", lt.Name)
	}
	if lt.Spec.Protocol != "https" {
		t.Error("spec not converted")
	}
	if lt.Status.Phase != "Ready" {
		t.Error("status not converted")
	}
}

func TestResource(t *testing.T) {
	gr := Resource("flowdefinitions")
	if gr.Group != Group {
		t.Errorf("expected group %s, got %s", Group, gr.Group)
	}
	if gr.Resource != "flowdefinitions" {
		t.Errorf("expected resource flowdefinitions, got %s", gr.Resource)
	}
}

func TestSchemeGroupVersion(t *testing.T) {
	if SchemeGroupVersion.Group != "fiso.io" {
		t.Errorf("expected group fiso.io, got %s", SchemeGroupVersion.Group)
	}
	if SchemeGroupVersion.Version != "v1alpha1" {
		t.Errorf("expected version v1alpha1, got %s", SchemeGroupVersion.Version)
	}
}

func TestFlowDefinitionSpec_DeepCopyNilTransform(t *testing.T) {
	spec := FlowDefinitionSpec{
		Source: SourceSpec{Type: "http"},
		Sink:   SinkSpec{Type: "http"},
	}

	var out FlowDefinitionSpec
	spec.DeepCopyInto(&out)
	if out.Transform != nil {
		t.Error("nil transform should remain nil after deep copy")
	}
	if out.Source.Type != "http" {
		t.Error("source type not copied")
	}
}
