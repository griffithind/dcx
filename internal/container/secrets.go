package container

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/secrets"
)

// DCXSecretPath returns the absolute in-container path for a dcx-managed
// secret by name. Example: DCXSecretPath("authorized_keys") →
// "/run/secrets/dcx/authorized_keys".
func DCXSecretPath(name string) string {
	return filepath.Join(agentproto.DCXSecretsDir, name)
}

// DCXSecretsDir returns the absolute in-container path of the dcx-managed
// secrets directory ("/run/secrets/dcx").
func DCXSecretsDir() string {
	return agentproto.DCXSecretsDir
}

// SecretsTmpfsMiB is the size of the /run/secrets tmpfs. It only caps what
// can be stored (tmpfs uses memory for what it holds), so it is generous;
// dcx's own SSH files need a few KiB of it.
const SecretsTmpfsMiB = 16

// secretsReserve is kept free for dcx's own files in /run/secrets/dcx.
const secretsReserve = 256 << 10

// checkSecretsFit fails with a clear message when the runtime secrets
// cannot fit the /run/secrets tmpfs, instead of a write error halfway.
func checkSecretsFit(list []secrets.Secret) error {
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

// MountSecretsToContainer copies secrets to /run/secrets in the specified container.
// Secrets are written as files with mode 0400 (read-only by owner).
// The owner parameter specifies the user who should own the secrets (e.g., "node", "root").
// This is a standalone function that can be called without a runtime instance.
func MountSecretsToContainer(ctx context.Context, engine Engine, containerName string, secretList []secrets.Secret, owner string) error {
	if len(secretList) == 0 {
		return nil
	}
	if err := checkSecretsFit(secretList); err != nil {
		return err
	}

	if containerName == "" {
		return fmt.Errorf("container name not set")
	}

	// Default to root if no owner specified
	if owner == "" {
		owner = "root"
	}

	for _, secret := range secretList {
		if err := secrets.ValidateName(secret.Name); err != nil {
			return fmt.Errorf("failed to write secret %q: %w", secret.Name, err)
		}
		// One exec per secret: the tmpfs directory (0755), then the file,
		// readable only by its owner.
		if err := engine.WriteFileInContainer(ctx, containerName, FileWrite{
			Path:    filepath.Join(agentproto.SecretsDir, secret.Name),
			Content: secret.Value,
			Owner:   owner,
			Mode:    "400",
			Dirs:    []string{agentproto.SecretsDir},
		}); err != nil {
			return fmt.Errorf("failed to write secret %q: %w", secret.Name, err)
		}
	}
	return nil
}

// DCXSecret is a dcx-managed tmpfs secret written into
// /run/secrets/dcx/<Name>. Used for the SSH authorized_keys list and the
// persistent host key. Unlike user secrets, these are readable by the
// SSH daemon (owned by root, mode 0444 for authorized_keys, 0400 for
// the host key).
type DCXSecret struct {
	Name  string // filename under /run/secrets/dcx/
	Value []byte
	Mode  string // e.g. "0444" or "0400"
	Owner string // "root" unless the file must be readable by the remote user
}

// MountDCXSecrets writes a set of dcx-managed secrets into the container's
// /run/secrets/dcx/ directory. Creates the subdirectory if necessary.
//
// Unlike MountSecretsToContainer (which handles user-defined secrets at
// /run/secrets/<name>), this function targets the dcx/ namespace so the
// two categories cannot collide.
func MountDCXSecrets(ctx context.Context, engine Engine, containerName string, mgmt []DCXSecret) error {
	if len(mgmt) == 0 {
		return nil
	}
	if containerName == "" {
		return fmt.Errorf("container name not set")
	}

	for _, sec := range mgmt {
		owner := sec.Owner
		if owner == "" {
			owner = "root"
		}
		mode := sec.Mode
		if mode == "" {
			mode = "0400"
		}
		if err := secrets.ValidateName(sec.Name); err != nil {
			return err
		}
		if err := engine.WriteFileInContainer(ctx, containerName, FileWrite{
			Path:    DCXSecretPath(sec.Name),
			Content: sec.Value,
			Owner:   owner,
			Mode:    mode,
			Dirs:    []string{agentproto.SecretsDir, DCXSecretsDir()},
		}); err != nil {
			return fmt.Errorf("write dcx secret %q: %w", sec.Name, err)
		}
	}
	return nil
}
