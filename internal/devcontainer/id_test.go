package devcontainer

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResourceName(t *testing.T) {
	assert.Equal(t, "my_app-abcdefgh", ResourceName("My App", "abcdefghijkl"))
	assert.Equal(t, "dcx-abcdefgh", ResourceName("!!!", "abcdefghijkl"))
	assert.Equal(t, "dcx_1st-abc", ResourceName("1st", "abc"))
}

// Two checkouts with the same devcontainer "name" must not share Docker
// resource names.
func TestResourceNameUniquePerWorkspace(t *testing.T) {
	a := ResourceName("Go", ComputeID(filepath.Join(t.TempDir(), "clone-a")))
	b := ResourceName("Go", ComputeID(filepath.Join(t.TempDir(), "clone-b")))
	assert.NotEqual(t, a, b)
}
