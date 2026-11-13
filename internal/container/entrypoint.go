package container

import (
	"context"
	"fmt"
	"strconv"
)

// runFeatureEntrypoints runs the feature entrypoints passed as positional
// parameters ("$1" is their count, the paths follow) and shifts them off,
// leaving the command in "$@". They run in install order without
// arguments, as the devcontainer CLI runs them: their own `exec "$@"` is
// then a no-op and they return. The paths are never interpolated into the
// script.
const runFeatureEntrypoints = `n=$1; shift; while [ "$n" -gt 0 ]; do "$1"; shift; n=$((n-1)); done`

// keepAlive is the overrideCommand entrypoint, running the feature
// entrypoints first.
func keepAlive(entrypoints []string) []string {
	if len(entrypoints) == 0 {
		return append([]string(nil), keepAliveEntrypoint...)
	}
	return append([]string{
		"/bin/sh", "-c",
		`trap "exit 0" TERM INT; ` + runFeatureEntrypoints + `; sleep infinity & wait`,
		"dcx-keepalive", strconv.Itoa(len(entrypoints)),
	}, entrypoints...)
}

// chainEntrypoints runs the feature entrypoints, then execs the command
// the container would have run without them (overrideCommand false).
func chainEntrypoints(entrypoints []string) []string {
	return append([]string{
		"/bin/sh", "-c",
		runFeatureEntrypoints + `; exec "$@"`,
		"dcx-entrypoint", strconv.Itoa(len(entrypoints)),
	}, entrypoints...)
}

// featureEntrypoints returns the entrypoint scripts to run at start: those
// recorded in the base image's metadata, then those of the features dcx
// installs (resolved.Entrypoints).
func (r *Provisioner) featureEntrypoints() []string {
	return r.resolved.Entrypoints
}

// prepareCommand records the command a chained entrypoint must exec: what
// the container would run without dcx touching its entrypoint. It is only
// needed when features declare entrypoints and overrideCommand is false.
func (r *Provisioner) prepareCommand(ctx context.Context, image string) error {
	r.command = nil
	if r.resolved.OverrideCommand || len(r.featureEntrypoints()) == 0 {
		return nil
	}
	imageCfg, err := r.engine.ImageConfig(ctx, image)
	if err != nil {
		return fmt.Errorf("read the command of %s: %w", image, err)
	}
	entrypoint, cmd := imageCfg.Entrypoint, imageCfg.Cmd
	if plan, ok := r.composePlan(); ok {
		svc, err := r.composeService(ctx, plan)
		if err != nil {
			return err
		}
		// Compose semantics: a service entrypoint drops the image CMD; a
		// service command replaces it.
		if svc.Entrypoint != nil {
			entrypoint, cmd = svc.Entrypoint, nil
		}
		if svc.Command != nil {
			cmd = svc.Command
		}
	}
	r.command = append(append([]string(nil), entrypoint...), cmd...)
	return nil
}
