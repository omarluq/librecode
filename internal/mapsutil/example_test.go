package mapsutil_test

import (
	"encoding/json"
	"fmt"

	"github.com/omarluq/librecode/internal/mapsutil"
)

// CloneOrEmpty always yields a non-nil map, so nil input still serializes as
// a JSON object instead of null.
func ExampleCloneOrEmpty() {
	serialized, err := json.Marshal(mapsutil.CloneOrEmpty(map[string]int(nil)))
	if err != nil {
		fmt.Println("error:", err)

		return
	}

	fmt.Println(string(serialized))
	// Output: {}
}

// ClonePreserveNil keeps the nil/empty distinction when copying optional maps.
func ExampleClonePreserveNil() {
	var unset map[string]string

	empty := map[string]string{}

	fmt.Println(mapsutil.ClonePreserveNil(unset) == nil)
	fmt.Println(mapsutil.ClonePreserveNil(empty) == nil)
	// Output:
	// true
	// false
}

// CloneOrNil collapses nil and empty input to nil, keeping `omitempty`
// fields out of serialized output.
func ExampleCloneOrNil() {
	empty := map[string]string{}

	fmt.Println(mapsutil.CloneOrNil(map[string]string(nil)) == nil)
	fmt.Println(mapsutil.CloneOrNil(empty) == nil)
	fmt.Println(mapsutil.CloneOrNil(map[string]string{"k": "v"}))
	// Output:
	// true
	// true
	// map[k:v]
}
