package mapsutil_test

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/omarluq/librecode/internal/mapsutil"
)

// mustPrintln writes example output to stdout for godoc, panicking on write
// errors so example bodies stay focused on the behavior under demonstration.
func mustPrintln(values ...any) {
	if _, err := fmt.Fprintln(os.Stdout, values...); err != nil {
		panic(err)
	}
}

// CloneOrEmpty always yields a non-nil map, so nil input still serializes as
// a JSON object instead of null.
func ExampleCloneOrEmpty() {
	serialized, err := json.Marshal(mapsutil.CloneOrEmpty(map[string]int(nil)))
	if err != nil {
		mustPrintln("error:", err)

		return
	}

	mustPrintln(string(serialized))
	// Output: {}
}

// ClonePreserveNil keeps the nil/empty distinction when copying optional maps.
func ExampleClonePreserveNil() {
	var unset map[string]string

	empty := map[string]string{}

	mustPrintln(mapsutil.ClonePreserveNil(unset) == nil)
	mustPrintln(mapsutil.ClonePreserveNil(empty) == nil)
	// Output:
	// true
	// false
}

// CloneOrNil collapses nil and empty input to nil, keeping `omitempty`
// fields out of serialized output.
func ExampleCloneOrNil() {
	empty := map[string]string{}

	mustPrintln(mapsutil.CloneOrNil(map[string]string(nil)) == nil)
	mustPrintln(mapsutil.CloneOrNil(empty) == nil)
	mustPrintln(mapsutil.CloneOrNil(map[string]string{"k": "v"}))
	// Output:
	// true
	// true
	// map[k:v]
}
