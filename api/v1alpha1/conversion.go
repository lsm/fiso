package v1alpha1

// ToFlowDefinition converts a FlowDefinitionCR to the lightweight FlowDefinition type.
func (cr *FlowDefinitionCR) ToFlowDefinition() *FlowDefinition {
	return &FlowDefinition{
		TypeMeta: TypeMeta{
			Kind:       cr.Kind,
			APIVersion: cr.APIVersion,
		},
		ObjectMeta: ObjectMeta{
			Name:        cr.Name,
			Namespace:   cr.Namespace,
			Labels:      cr.Labels,
			Annotations: cr.Annotations,
			Generation:  cr.Generation,
		},
		Spec:   cr.Spec,
		Status: cr.Status,
	}
}

// ToLinkTarget converts a LinkTargetCR to the lightweight LinkTarget type.
func (cr *LinkTargetCR) ToLinkTarget() *LinkTarget {
	return &LinkTarget{
		TypeMeta: TypeMeta{
			Kind:       cr.Kind,
			APIVersion: cr.APIVersion,
		},
		ObjectMeta: ObjectMeta{
			Name:        cr.Name,
			Namespace:   cr.Namespace,
			Labels:      cr.Labels,
			Annotations: cr.Annotations,
			Generation:  cr.Generation,
		},
		Spec:   cr.Spec,
		Status: cr.Status,
	}
}
