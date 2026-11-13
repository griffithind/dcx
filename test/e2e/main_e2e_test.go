//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
)

// TestMain isolates the run from the developer's environment: dcx writes
// SSH config into a temporary directory instead of ~/.ssh, and the images
// the tests built (dcx/<test workspace id>:*) are removed at the end.
func TestMain(m *testing.M) {
	if _, err := helpers.UseIsolatedSSHDir(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: cannot create isolated SSH dir:", err)
		os.Exit(1)
	}
	code := m.Run()
	helpers.RemoveTestImages()
	os.Exit(code)
}
