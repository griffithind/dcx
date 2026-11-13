package devcontainer

import (
	"encoding/json"
	"testing"
)

// hashPinConfig uses every form of the fields whose Go types are typed
// rather than interface{}: lifecycle commands (string, array, object),
// ports (numbers, strings, scalar appPort), mounts (string, object) and
// GPU requirements.
const hashPinConfig = `{
	"name": "pin",
	"image": "alpine:3",
	"onCreateCommand": "echo create",
	"updateContentCommand": ["npm", "ci"],
	"postCreateCommand": {"a": "echo a", "b": ["echo", "b"]},
	"forwardPorts": [3000, "8080:80", "db:5432"],
	"appPort": 9000,
	"mounts": ["source=vol,target=/data,type=volume", {"source": "/h", "target": "/c", "type": "bind"}],
	"hostRequirements": {"cpus": 2, "gpu": "optional"},
	"features": {"ghcr.io/devcontainers/features/node:1": {"version": "20"}}
}`

// hashPinJSON is normalizedConfigJSON(hashPinConfig) as computed before the
// config fields were typed. It must not change: the config hash is the
// staleness key, and a different encoding would mark every existing
// container stale.
const hashPinJSON = `{"image":"alpine:3","features":{"ghcr.io/devcontainers/features/node:1":{"version":"20"}},"forwardPorts":[3000,"8080:80","db:5432"],"appPort":9000,"mounts":[{"source":"vol","target":"/data","type":"volume"},{"source":"/h","target":"/c","type":"bind"}],"onCreateCommand":"echo create","updateContentCommand":["npm","ci"],"postCreateCommand":{"a":"echo a","b":["echo","b"]}}`

func TestConfigHashEncodingIsStable(t *testing.T) {
	var cfg DevContainerConfig
	if err := json.Unmarshal([]byte(hashPinConfig), &cfg); err != nil {
		t.Fatal(err)
	}
	got, err := normalizedConfigJSON(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != hashPinJSON {
		t.Errorf("normalizedConfigJSON changed:\n got: %s\nwant: %s", got, hashPinJSON)
	}
}
