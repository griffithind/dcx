package container

import (
	"fmt"

	"github.com/griffithind/dcx/internal/secrets"
)

// SecretsTmpfsMiB is the size of the /run/secrets tmpfs. It only caps what
// can be stored (tmpfs uses memory for what it holds), so it is generous;
// dcx's own SSH files need a few KiB of it.
const SecretsTmpfsMiB = 16

// secretsReserve is kept free for dcx's own files in /run/secrets/dcx.
const secretsReserve = 256 << 10

// CheckSecretsFit fails with a clear message when the runtime secrets
// cannot fit the /run/secrets tmpfs, before anything is written. The agent
// writes them (agentctl.Provision).
func CheckSecretsFit(list []secrets.Secret) error {
	total := 0
	for _, s := range list {
		total += len(s.Value)
	}
	if limit := SecretsTmpfsMiB<<20 - secretsReserve; total > limit {
		return fmt.Errorf("runtime secrets total %d KiB, more than the %d MiB /run/secrets tmpfs holds; mount large files with \"mounts\" instead",
			total>>10, SecretsTmpfsMiB)
	}
	return nil
}
