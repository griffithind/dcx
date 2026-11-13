package container

import (
	"context"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/secrets"
)

// recordingEngine counts the writes MountSecretsToContainer makes.
type recordingEngine struct {
	Engine
	writes int
}

func (e *recordingEngine) WriteFileInContainer(context.Context, string, string, []byte, string) error {
	e.writes++
	return nil
}

// Runtime secrets that cannot fit the /run/secrets tmpfs fail upfront with
// a clear message (R21), before anything is written.
func TestMountSecretsRejectsOversizedSecrets(t *testing.T) {
	fake := &recordingEngine{}
	big := secrets.Secret{Name: "blob", Value: make([]byte, SecretsTmpfsMiB<<20)}
	err := MountSecretsToContainer(context.Background(), fake, "c1", []secrets.Secret{big}, "dev")
	if err == nil || !strings.Contains(err.Error(), "/run/secrets tmpfs") {
		t.Fatalf("want a size error, got %v", err)
	}
	if fake.writes != 0 {
		t.Errorf("%d secrets were written before the size check", fake.writes)
	}
}
