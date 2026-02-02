package v1alpha1

// ToFlowDefinition converts a FlowDefinitionCR to the lightweight FlowDefinition type.
func (cr *FlowDefinitionCR) ToFlowDefinition() *FlowDefinition {
	return &FlowDefinition{
		TypeMeta: TypeMeta{
			Kind:       cr.TypeMeta.Kind,
			APIVersion: cr.TypeMeta.APIVersion,
		},
		ObjectMeta: ObjectMeta{
			Name:        cr.ObjectMeta.Name,
			Namespace:   cr.ObjectMeta.Namespace,
			Labels:      cr.ObjectMeta.Labels,
			Annotations: cr.ObjectMeta.Annotations,
			Generation:  cr.ObjectMeta.Generation,
		},
		Spec:   cr.Spec,
		Status: cr.Status,
	}
}

// ToLinkTarget converts a LinkTargetCR to the lightweight LinkTarget type.
func (cr *LinkTargetCR) ToLinkTarget() *LinkTarget {
	return &LinkTarget{
		TypeMeta: TypeMeta{
			Kind:       cr.TypeMeta.Kind,
			APIVersion: cr.TypeMeta.APIVersion,
		},
		ObjectMeta: ObjectMeta{
			Name:        cr.ObjectMeta.Name,
			Namespace:   cr.ObjectMeta.Namespace,
			Labels:      cr.ObjectMeta.Labels,
			Annotations: cr.ObjectMeta.Annotations,
			Generation:  cr.ObjectMeta.Generation,
		},
		Spec:   cr.Spec,
		Status: cr.Status,
	}
}
