package container

import (
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/secrets"
)

// Runtime secrets that cannot fit the /run/secrets tmpfs fail upfront with
// a clear message (R21), before anything is written.
func TestCheckSecretsFitRejectsOversizedSecrets(t *testing.T) {
	big := secrets.Secret{Name: "blob", Value: make([]byte, SecretsTmpfsMiB<<20)}
	err := CheckSecretsFit([]secrets.Secret{big})
	if err == nil || !strings.Contains(err.Error(), "/run/secrets tmpfs") {
		t.Fatalf("want a size error, got %v", err)
	}
	if err := CheckSecretsFit([]secrets.Secret{{Name: "small", Value: []byte("x")}}); err != nil {
		t.Fatalf("a small secret fits: %v", err)
	}
}
