package model

// CloneTokenContributors copies token contributor slices, preserving nil/empty-as-nil semantics.
func CloneTokenContributors(contributors []TokenContributor) []TokenContributor {
	if len(contributors) == 0 {
		return nil
	}

	cloned := make([]TokenContributor, len(contributors))
	copy(cloned, contributors)

	return cloned
}
