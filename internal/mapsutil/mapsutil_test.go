package mapsutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/mapsutil"
)

const testMapKey = "key"

func TestCloneOrEmpty(t *testing.T) {
	t.Parallel()

	empty := mapsutil.CloneOrEmpty(map[string]string(nil))
	assert.NotNil(t, empty)
	assert.Empty(t, empty)

	original := map[string]string{testMapKey: "value"}
	cloned := mapsutil.CloneOrEmpty(original)
	cloned[testMapKey] = "changed"
	assert.Equal(t, "value", original[testMapKey])
}

func TestClonePreserveNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, mapsutil.ClonePreserveNil(map[string]int(nil)))

	empty := mapsutil.ClonePreserveNil(map[string]int{})
	assert.NotNil(t, empty)
	assert.Empty(t, empty)

	original := map[string]int{testMapKey: 1}
	cloned := mapsutil.ClonePreserveNil(original)
	cloned[testMapKey] = 2
	assert.Equal(t, 1, original[testMapKey])
}

func TestCloneOrNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, mapsutil.CloneOrNil(map[string]int(nil)))
	assert.Nil(t, mapsutil.CloneOrNil(map[string]int{}))

	original := map[string]int{testMapKey: 1}
	cloned := mapsutil.CloneOrNil(original)
	cloned[testMapKey] = 2
	assert.Equal(t, 1, original[testMapKey])
}

func TestIntMapToAnyMap(t *testing.T) {
	t.Parallel()

	original := map[string]int{"a": 1}
	cloned := mapsutil.IntMapToAnyMap(original)
	cloned["a"] = 2

	assert.Equal(t, 1, original["a"])
	assert.Equal(t, map[string]any{"a": 2}, cloned)
}
