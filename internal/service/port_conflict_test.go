package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The hint names the port, and suggests --recreate only for the SSH port
// (the only one a recreate changes).
func TestExplainPortConflict(t *testing.T) {
	assert.NoError(t, explainPortConflict(nil, 0))
	other := errors.New("boom")
	assert.Same(t, other, explainPortConflict(other, 2222))

	ssh := explainPortConflict(errors.New("Bind for 127.0.0.1:2222 failed: port is already allocated"), 2222)
	assert.Contains(t, ssh.Error(), "the SSH port 2222")
	assert.Contains(t, ssh.Error(), "--recreate")

	user := explainPortConflict(errors.New("listen tcp4 127.0.0.1:3000: bind: address already in use"), 2222)
	assert.Contains(t, user.Error(), "host port 3000")
	assert.NotContains(t, user.Error(), "--recreate")

	unknown := explainPortConflict(errors.New("port is already allocated"), 0)
	assert.NotContains(t, unknown.Error(), "--recreate")
}
