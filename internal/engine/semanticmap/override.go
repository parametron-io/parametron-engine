package semanticmap

func (policy *OverridePolicy) OverridesAllowed() bool {
	if policy == nil {
		return false
	}
	return policy.AllowsAdapterOverrides() || policy.AllowsProjectOverrides() || policy.AllowsUserOverrides()
}

func (policy *OverridePolicy) AllowsAdapterOverrides() bool {
	return policy != nil && policy.AllowAdapterOverrides != nil && *policy.AllowAdapterOverrides
}

func (policy *OverridePolicy) AllowsProjectOverrides() bool {
	return policy != nil && policy.AllowProjectOverrides != nil && *policy.AllowProjectOverrides
}

func (policy *OverridePolicy) AllowsUserOverrides() bool {
	return policy != nil && policy.AllowUserOverrides != nil && *policy.AllowUserOverrides
}

func ValidateOverridePolicy(policy *OverridePolicy) error {
	problems := validateOverridePolicy(policy)
	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}
