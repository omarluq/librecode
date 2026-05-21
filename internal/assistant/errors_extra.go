package assistant

import "github.com/samber/oops"

func emptyProviderResponseError(code string) error {
	return oops.In("assistant").Code(code).Errorf("provider returned an empty response")
}
