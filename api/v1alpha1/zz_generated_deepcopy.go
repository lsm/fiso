package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto copies all properties into another FlowDefinitionCR.
func (in *FlowDefinitionCR) DeepCopyInto(out *FlowDefinitionCR) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy creates a deep copy of FlowDefinitionCR.
func (in *FlowDefinitionCR) DeepCopy() *FlowDefinitionCR {
	if in == nil {
		return nil
	}
	out := new(FlowDefinitionCR)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *FlowDefinitionCR) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies all properties into another FlowDefinitionCRList.
func (in *FlowDefinitionCRList) DeepCopyInto(out *FlowDefinitionCRList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]FlowDefinitionCR, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a deep copy of FlowDefinitionCRList.
func (in *FlowDefinitionCRList) DeepCopy() *FlowDefinitionCRList {
	if in == nil {
		return nil
	}
	out := new(FlowDefinitionCRList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *FlowDefinitionCRList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies all properties into another LinkTargetCR.
func (in *LinkTargetCR) DeepCopyInto(out *LinkTargetCR) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy creates a deep copy of LinkTargetCR.
func (in *LinkTargetCR) DeepCopy() *LinkTargetCR {
	if in == nil {
		return nil
	}
	out := new(LinkTargetCR)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *LinkTargetCR) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies all properties into another LinkTargetCRList.
func (in *LinkTargetCRList) DeepCopyInto(out *LinkTargetCRList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]LinkTargetCR, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a deep copy of LinkTargetCRList.
func (in *LinkTargetCRList) DeepCopy() *LinkTargetCRList {
	if in == nil {
		return nil
	}
	out := new(LinkTargetCRList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *LinkTargetCRList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies FlowDefinitionSpec fields.
func (in *FlowDefinitionSpec) DeepCopyInto(out *FlowDefinitionSpec) {
	*out = *in
	in.Source.DeepCopyInto(&out.Source)
	if in.Transform != nil {
		out.Transform = new(TransformSpec)
		*out.Transform = *in.Transform
	}
	in.Sink.DeepCopyInto(&out.Sink)
	out.ErrorHandling = in.ErrorHandling
}

// DeepCopyInto copies SourceSpec fields.
func (in *SourceSpec) DeepCopyInto(out *SourceSpec) {
	*out = *in
	if in.Config != nil {
		out.Config = make(map[string]string, len(in.Config))
		for k, v := range in.Config {
			out.Config[k] = v
		}
	}
}

// DeepCopyInto copies SinkSpec fields.
func (in *SinkSpec) DeepCopyInto(out *SinkSpec) {
	*out = *in
	if in.Config != nil {
		out.Config = make(map[string]string, len(in.Config))
		for k, v := range in.Config {
			out.Config[k] = v
		}
	}
}

// DeepCopyInto copies LinkTargetSpec fields.
func (in *LinkTargetSpec) DeepCopyInto(out *LinkTargetSpec) {
	*out = *in
	if in.Auth != nil {
		out.Auth = new(LinkAuthSpec)
		*out.Auth = *in.Auth
	}
	if in.CircuitBreaker != nil {
		out.CircuitBreaker = new(CircuitBreakerSpec)
		*out.CircuitBreaker = *in.CircuitBreaker
	}
	if in.Retry != nil {
		out.Retry = new(RetrySpec)
		*out.Retry = *in.Retry
	}
	if in.AllowedPaths != nil {
		out.AllowedPaths = make([]string, len(in.AllowedPaths))
		copy(out.AllowedPaths, in.AllowedPaths)
	}
}
