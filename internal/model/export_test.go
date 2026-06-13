package model

// MergeModelCatalogsForTest exposes catalog merge precedence for tests.
func MergeModelCatalogsForTest(catalogs ...[]Model) []Model {
	return mergeModelCatalogs(catalogs...)
}
