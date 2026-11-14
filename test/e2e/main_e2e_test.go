//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
)

// TestMain isolates the run from the developer's environment: dcx writes
// its SSH config and state (~/.ssh, ~/.dcx) into temporary directories, and
// the images the tests built (dcx/<test workspace id>:*) are removed at the
// end.
func TestMain(m *testing.M) {
	cleanup, err := helpers.Isolate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: cannot isolate the run:", err)
		os.Exit(1)
	}
	code := m.Run()
	helpers.RemoveTestImages()
	cleanup()
	os.Exit(code)
}
