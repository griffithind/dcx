# dcx second audit (after the Phase 0–8 refactor)

**Question:** should dcx be re-engineered or re-architected? Is it a clean codebase? How much naming and structure is left over from the past, and should it be cleaned up aggressively?

**Short answer:** no rewrite and no new architecture. The layering (cli → service → container → domain) is real. The import graph has no cycles and points the right way. The packages refactored in Phases 2–8 are clean. What remains falls into three groups:

1. **Unrefactored corners with real bugs.** `internal/env` and feature-fetch caching hold two root-shell injection bugs, a cache that never works and a `dcx lock` mode that passes when it shouldn't.
2. **Three leaky seams.** An incomplete `container.Engine`, start/stop decisions spread over the CLI, service and runtime, and a `DevContainerService` that still carries agent/SSH provisioning.
3. **Leftover vocabulary and dead weight.** About half the label schema is never written, there are five structs meaning "container info", history comments, stale `CLAUDE.md` lines, and about 15 tests that assert nothing.

Group 3 should be cleaned up aggressively, because it is mostly deletion: cheap and low-risk. Groups 1 and 2 are targeted fixes, a few hundred lines each.

## How this was produced

- **Measured:** package sizes, test coverage, cyclomatic complexity (gocyclo, average 3.98), function lengths, the internal import graph and `docker` calls outside the engine.
- **Reviewed independently:** three reviews, one each for naming and legacy, architecture and boundaries, and code and test quality.
- **Checked:** every finding below marked "confirmed" was checked against the code. One review claim was dropped because the code path can't run: the Plan/Up disagreement on `StateStopped`, since `StateStopped` is never produced.

## Numbers

| Package | Lines (source) | Coverage | Notes |
|---|---|---|---|
| cli | 3,400 | 9.6% | Most tests are tautological. Complexity peaks are `displayPlan` (51) and `runStatus` (35). |
| devcontainer | 3,094 | 72.6% | Big but cohesive: small files, golden tests. Doesn't need splitting. |
| container | 2,564 | 28.1% | Compose paths shell out, so they can't use the fake. |
| features | 2,221 | 40.7% | OCI fetch and cache are untested (no `httptest`). |
| service | 1,448 | 31.1% | God object (see below). |
| ssh/server (agent) | 1,356 | 49.2% | Clean since Phase 7. |
| build | 883 | 20.9% | Runs its own `docker` calls. |
| env | 330 | 17.9% | Never refactored; worst code in the repo. |
| ssh/deploy | 151 | 0% | Shells out to `docker` directly. |

Pure packages (shell, shortcuts, lockfile, state, ssh, secrets, hostconfig) are all above 80%.

## 1. Confirmed bugs (fix first)

| # | Severity | Where | Problem |
|---|---|---|---|
| B1 | **High (security)** | `env/probe.go:195-215` | `writeCache` puts the probed user environment into a root `sh -c` inside single quotes (`echo '%s' > …`). The probe output comes from the container user's shell profile, so a value containing `'` executes as root in the container. The resulting error is discarded. |
| B2 | **High (security)** | `env/patch.go:50-66` | `/etc/environment` is written by splicing values into a heredoc, escaping only `"`. A value containing a newline or the line `DCXEOF` breaks out into a root shell. Map order makes the output random. It is append-once behind a marker, so changed `remoteEnv`/`containerEnv` values are never updated. |
| B3 | High | `env/probe.go:122-192` | The probe cache reads container labels but writes files under `/var/lib/dcx`, and the labels are never written. It never hits, so every `up` re-probes. |
| B4 | High | `service/devcontainer.go:929-985`, `features/lock.go:89` | `dcx lock --verify` reports "verified" when there is no lockfile (`VerifyLockfile(…, nil)` returns no mismatches). `--frozen` passes on an init-marker lockfile. The Verify and Frozen branches are copy-pasted. |
| B5 | High | `features/resolver.go:226-268` (OCI), `300-337` (HTTP) | Features are extracted straight into the cache directory and never cleaned up on failure. A later run trusts a partial cache when no digest file exists ("backwards compatibility"). Concurrent runs race on the same path. |
| B6 | Medium | `cli/status.go:171` | The `status --detailed` labels section filters on `dcx.`, but every key starts with `com.griffithind.dcx.`, so it never prints. |
| B7 | Medium | `service/devcontainer.go:332` | The authorized_keys "drift" message compares against `SSHAuthorizedKeysSHA256`, a label that is never written, so it never fires. |
| B8 | Medium | `cli/list.go:93,104` | `list --json` reports `CreatedAt` as `time.Now()`. |
| B9 | Medium | `container/docker.go:761` | `Docker.Logs` drops the `docker logs` error, so "no such container" shows up as empty output. |
| B10 | Medium | `cli/stop.go:22`, `state/state.go:110` | User-facing text tells users to run `dcx start`, which doesn't exist. |
| B11 | Low | `status.go:107,111,164`, `debug.go:335`, `list.go:100` | Unchecked `[:12]` slicing. A short or hand-edited hash label panics `dcx status`. |
| B12 | Low | `features/resolver.go` | OCI fetch: the token error is dropped, the token URL is built with `Sprintf`, and `io.ReadAll` has no size limit. Extraction has no total-size limit; a chain of symlinks can pass the lexical escape check, though writes through it are still blocked. |

## 2. Architecture: three seams, not a rewrite

### 2.1 `container.Engine` is incomplete (the root of most low coverage)

`docker` is still run directly by:
- `build/dockerfile.go` (buildx build, plus duplicate `ImageExists`/`PullImage`/`GetImageID`/`GetImageLabels`)
- `container/compose.go` (`ComposeProject.run`)
- the package-level `container.Exec`/`ExecOutput` used by `env`
- `env/probe.go` (`docker inspect`)
- `ssh/deploy/deploy.go` (exec, cp, kill, ping)
- `cli/doctor.go`

So `Up`'s build, deploy and env steps all escape `containertest.Fake`, which is why B1–B3 went unnoticed and why service, build, env and deploy coverage is so low.

**Fix:** extend `Engine` with `BuildImage`, `CopyToContainer`, `ExecAs(user)` and `Compose(project, args…)`. Make `build`, `ssh/deploy` and `env` take an `Engine`. Delete `container/exec.go`. After this, `docker.go` is the only file that runs `docker`. depguard can enforce it by denying `os/exec` outside `container` and `ssh/server`.

### 2.2 Start/stop and "what should Up do" live in several places

- **Stop** is implemented in `cli/stop.go`, `cli/restart.go` and `service.DownWithIDs`, plus an unused `UnifiedRuntime.Stop`/`Down`.
- **The shutdownAction check** is duplicated in `stop.go` and `restart.go`, each reloading the config in the CLI.
- **`restart --rebuild`** sets package globals and calls `runUp`.
- **Plan vs Up:** `state.DeterminePlanAction` claims to be the single source of truth, but only `Plan()` uses it; `Up`'s `stepContainer` has its own switch. Today they differ when setup is incomplete (Plan says "nothing to do", Up resumes), and nothing keeps them from drifting.

**Fix:**
- Add `Service.Stop`/`Restart`, which own the shutdownAction check; `DownWithIDs` reuses `Stop`.
- `stepContainer` switches on `DeterminePlanAction`, extended with the "setup incomplete" case.
- Delete the `ContainerRuntime` interface and `UnifiedRuntime.Stop`/`Down`.
- What remains of `UnifiedRuntime` creates environments: image layers → `ContainerSpec` → create or compose up. Rename it `container.Provisioner`.

### 2.3 `DevContainerService` is a god object (~1,450 lines)

Besides orchestration, it holds:
- the agent launch protocol (setup, launch, readiness, log tail)
- SSH secret mounting
- host authorized-key collection (ed25519, agent dial)
- SSH config writing
- image-metadata merge
- environment patching
- lockfile generation

Agent provisioning is split: install is in `ssh/deploy`, launch in `service`, and `up.go` stitches them together. `GetIdentifiers()` re-parses devcontainer.json about six times per `up`, and its error fallback silently produces a different identity.

**Fix:**
- Move agent install, secrets, setup, launch, readiness and outdated-agent stop into one package (`internal/agentctl`, or by growing `ssh/deploy`) behind `Provision(ctx, Engine, info, opts)`.
- Move authorized-key collection into `internal/ssh`.
- Compute `Identifiers` once from the resolved config and pass them along.

The service then becomes about 500 lines of orchestration, testable against the fake.

### 2.4 The CLI holds logic and builds its own dependencies (medium)

- **Concrete dependencies:** `CLIContext.Docker` is a `*container.Docker`. `plan`, `list`, `clean`, `doctor` and `debug` construct their own Docker client, and several use `context.Background()`, so Ctrl-C doesn't cancel feature fetches.
- **Flags as globals:** flags are package globals.
- **Logic in commands:** `status` has two load paths; `list` reimplements the state inventory; `displayPlan` and `runStatus` dump `RawConfig` with random map order and their own plan-type logic.

**Fix:**
- One composition root, with commands receiving the engine and service.
- Per-command option structs.
- `Service.Status()`/`List()` return data, and the CLI only renders it.
- Shared render helpers, plus golden output tests.

### Target layout (incremental)

```
cmd/dcx            composition root: engine → service → cli
internal/cli       flags → options → service call → render (imports service, ui, report)
internal/service   Up / Stop / Restart / Down / Plan / Status / List / Build / Lock
internal/agentctl  agent install, secrets, setup, launch, readiness   (from ssh/deploy + service)
internal/container Engine (containers, exec-as, cp, images, build, compose) — the only docker caller
                   Provisioner (ex-UnifiedRuntime): layers → ContainerSpec → create / compose up
internal/build     Dockerfile / feature-staging / UID generation, no exec
internal/env       pure: /etc/environment content, probe parsing; I/O through Engine
internal/agent     the dcx-agent (renamed from ssh/server)
internal/ssh       host keys, known_hosts, authorized keys; hostconfig/, client/ (hooks)
devcontainer, features, state, lockfile, lifecycle, shell, agentproto   unchanged
```

`devcontainer` stays one package. It is large, but each file has one job, and the golden tests cover the whole resolution.

## 3. Naming and legacy leftovers

### Delete (dead or never written)

- **Labels never written by `buildLabels`:**
  - CreatedAt, CreatedBy, LastStartedAt, LifecycleState (and the `LifecycleState*` constants)
  - FeaturesConfig, CacheData, CacheFeatureDigests, CacheProbedEnv(Hash)
  - SSHHostPort, SSHBindAddress, SSHAllowedClientIPs, SSHAuthorizedKeysSHA256

  Delete them with the code that reads them (B3, B7).
- **`state`:**
  - `StateStopped`, which is never produced.
  - The test-only helpers `IsUsable`, `NeedsRecreate`, `CanStart/Stop/Exec`, `IsAbsent` and `GetRecovery`/`Recovery*`.
  - `ContainerClient`'s unused methods.
  - The ignored `projectName` parameter of `GetStateWithProject*` (6 callers).
- **Unused types and methods:**
  - In `container`: `ContainerInfo`, `ExecOptions` (kept alive only by a tautological test), `ImageBuildOptions`/`BuildImage`, `KillContainer`, `SimpleExecInContainer` and `CopyToContainer`.
  - Elsewhere: `CLIBuilder.ResolveImage`, `ResolvedDevContainer.Labels` (removing it drops the `devcontainer` → `state` import), `LifecycleHooks`/`HookCommand`, `lastResolved` and the no-op `Close()`.
- **Legacy-prefix handling:** `io.github.dcx.` in `IsPrimaryContainer`, and `LegacyLabels` in `debug`. Lookup already filters on the new labels, so these can't match.
- **History comments:** "replaces the previous X" notes in about 12 files, and the stdio/ProxyCommand text in hostconfig.

### Rename

- **Types and packages:**
  - `UnifiedRuntime` → `Provisioner`.
  - `internal/ssh/server` → `internal/agent`. It is the whole dcx-agent; the current name is why `CLAUDE.md` still says `internal/agent`.
  - `cli/validate.go` → `state_guard.go`.
  - `ssh/client` → `ssh/hookexec` is optional.
- **One concept, several names:**
  - **"ProjectName"** means the Docker resource name in `Identifiers`/`DevContainerID`, but the workspace name in `list --json`. Use `ResourceName` and `Name`.
  - **Identity** is duplicated: `DevContainerID` is copied field by field into `service.Identifiers`. Keep one.
  - **Container info** has five structs; keep `state.ContainerSummary`/`Details`/`Info`, and give `Info` the same field names as the labels (`BuildMethod` rather than `Plan`).
  - **Options types are duplicated across layers:** `service.UpOptions.Build` always equals `Rebuild`, and pulling has three names (`Pull`, `ForcePull`, `PullForMetadata`).
- **Vocabulary:** pick "workspace" for identity (not environment/project/devcontainer) and "plan" for the build method.
- **Conventions:** drop the `Get` prefixes (`GetImageID`, `GetIdentifiers`, `GetStateManager`, …).

### Fix text

- `dcx start` → `dcx up` (B10).
- The root command help says "Docker Engine API" (dcx uses the CLI).
- `validate.go` mentions exec/shell.
- `CLAUDE.md` still lists `internal/agent/` and `agent.Execute()`.
- Stale comments: "uses singleton", "SSH proxy directory".

## 4. Code and test quality

**Clean:**
- service/up, container/spec, compose_override, shell, hostconfig, ssh/server and the tar extractor
- golden resolve tests
- Fake-based service tests

Errors are mostly wrapped consistently, and there are no panics apart from B11.

**Below standard:**
- **`internal/env`:** B1–B3, and it ignores the project's own engine and shell rules.
- **Display commands:** `plan`, `status` and `doctor` are long, use random map order and read `RawConfig` rather than resolved values.
- **Feature fetch:** caching is not atomic and is duplicated between OCI and HTTP.
- **Loose types:** `interface{}` for the six lifecycle hook fields and for ports in `DevContainerConfig`; typed `LifecycleCommand`/`PortSpec` fields would remove several type switches.
- **Magic strings:** `/var/lib/dcx` is hard-coded in three places despite `agentproto.StateDir`.

**Tests look bigger than they are:**
- **`container/docker_test.go`:** 5 of 7 tests check struct literals.
- **`cli/validate_test.go` and `context_test.go`:** tautological.
- **`commands_test.go`:** mostly checks that flags exist.
- **Untested:** the code with the confirmed bugs (env, `Lock` modes, OCI fetch and cache, deploy, `displayPlan`/`runStatus`).

## Status

**Step 1 (bugs): done.**
- **B1–B3:** the probe cache is removed; it never hit, and its writer was the injection. `/etc/environment` is now a dcx-managed block, rendered in Go (sorted; invalid names and multi-line values are skipped with a warning) and written through the engine on every `up`/`restart`. `/etc/profile` patching passes its marker path as an argument. The probe runs as the effective user, and the patch uses resolved env, not raw config.
- **B4:** verify and frozen fail without a real lockfile; the branches are merged.
- **B5:** features are fetched into a temp dir and renamed into place. An entry counts only once its digest file exists; the "no digest means trusted" path is gone.
- **B6, B8, B9, B10, B11:** fixed (`state.Prefix` filter with sorted labels, real `Created` from inspect, `CloseWithError`, `dcx up` in the text, `shortID`).
- **B7:** the dead check is removed, along with `SHA256Hex`.
- **B12:** fixed except chained-symlink resolution. Manifest/blob/error reads are bounded, extraction is capped at 1 GiB, the token URL is built with `url.Values`, and token failures appear in 401/403 errors. Chained symlinks are still blocked from being written through.

Regression tests: env patch (unit), lock modes, feature cache, and two e2e tests. Run against main's binary, the e2e tests fail, and one reproduces the root injection (`/tmp/pwned-probe` created).

**Step 2 (deletion pass): done.** About 1,400 net lines removed:
- **Labels:** the 13 labels dcx never wrote are gone, along with the legacy `io.github.dcx.` prefix handling and `debug`'s legacy-label notice.
- **`state`:** `StateStopped` is gone, along with the test-only helpers, the recovery types and the three unused `ContainerClient` methods (now on `Engine`). `GetStateWithProject*` became `GetState`/`GetStateWithHash` without the ignored parameter.
- **`container`:** removed `ContainerRuntime`, `ContainerInfo`, `ExecOptions`, `ImageBuildOptions`/`BuildImage` (and `sshAgentAvailable` with it), `KillContainer`, `SimpleExecInContainer`, `CopyToContainer`, `ExecResult`, and `UnifiedRuntime.Stop`/`Down`.
- **Elsewhere:** removed `CLIBuilder.ResolveImage`, `ResolvedDevContainer.Labels` (`devcontainer` no longer imports `state`), `LifecycleHooks`/`HookCommand`, `lastResolved`, the no-op `Service.Close` and `HasSSHConfig` (its tests now cover `Aliases`).
- **Tests:** about 20 tautological tests deleted; added a real `parseImageSize` test.
- **Text:** history comments rewritten to say what the code does. Help text, stale comments and `CLAUDE.md` fixed (agent entry point, state enum, label list).

**Step 3 (complete the Engine): done.** Only `container/docker.go` runs `docker` now:
- **Build:** `build` defines a `build.Engine` (BuildImage, ImageExists, GetImageLabels), which `container.Engine` satisfies; `build` can't import `container`, which imports it. `CLIBuilder` became `Builder`, with no `os/exec`, and its duplicate image-inspect methods are gone. `Docker.BuildImage` renders a deterministic `buildx` command line.
- **Env:** `env.Prober` uses the new `Engine.ExecOutputAs(user)`. The package-level `container.Exec`/`ExecOutput` (`container/exec.go`) is deleted.
- **Agent deploy:** `ssh/deploy` is now `Deploy`/`StopOutdated(ctx, engine, container)`, through `ExecAsRoot`, `ExecOutput`, `ExecInContainer` and the re-added `Engine.CopyToContainer`.
- **Compose:** `ComposeProject` is data. Its Start/Stop/Down take an Engine, and every compose command runs through `Engine.Compose`/`ComposeOutput`.
- **Doctor:** `doctor` checks `docker compose` through `Docker.ComposeVersion`. A standalone `docker-compose` no longer counts as OK, since dcx never uses it, and the empty-output panic is gone.
- **Lint rule:** depguard denies `os/exec` everywhere except `container/docker.go` and packages that run other host programs (lifecycle, secrets, selinux, host-requirement probes, cli, the agent, tests).
- **Coverage:** build 21% → 50%, env 18% → 93%, ssh/deploy 0% → 75%, container 28% → 35%.

**Step 4 (one reconcile decision): done.**
- **`dcx plan` is removed** (owner's call: unused). With it go `Service.Plan`, `PlanResult`/`PlanOptions`, `state.DeterminePlanAction`/`PlanAction*` and `displayPlan`, the most complex function in the codebase (cyclomatic 51). `dcx status` still shows state and staleness.
- **One decision for `up`:** `decideUp(state, setupComplete, opts)` in `service/up.go` returns none, resume, start, create or recreate. `Up` and `stepContainer` both switch on it, and `--recreate`/`--rebuild` now also apply to a stopped container. It has a table test.
- **`Service.Stop`/`Restart`** own the state check, the `shutdownAction` rule (read without feature resolution, so stop works offline) and the single-vs-compose stop. `DownWithIDs` reuses the same stop. `cli/stop.go` and `cli/restart.go` only render outcomes, and `restart --rebuild` no longer sets `up`'s package globals.
- **`UnifiedRuntime` → `Provisioner`** (`provisioner.go`): it builds image layers and creates or starts environments.
- **Also removed:** the unused `cli.CheckState` and `ui.IsVerbose`.

**Step 5 (extract agent provisioning): done.**
- **New `internal/agentctl`:** `ssh/deploy` moved in (`install.go`), along with the service's agent secret writing, root setup, launch and readiness code. `agentctl.Provision(ctx, engine, rep, Options)` is now the single call `up` makes for the agent. The launch, secret and timeout tests moved with it.
- **`ssh.AuthorizedKeys`** is the old `collectAuthorizedKeys`, which moved to `internal/ssh` along with the fallback-key and agent-key helpers and their test.
- **Identifiers:** `Service.Identifiers()` computes them once per service and returns no error, because it couldn't fail. Callers no longer re-parse devcontainer.json each time.
- **Rename:** `internal/ssh/server` → `internal/agent`, the dcx-agent itself. The depguard rules and `CLAUDE.md` follow.
- **Size:** the service drops from about 1,450 to about 1,100 lines across `devcontainer.go` and `up.go`. Service coverage dips from 33% to 28%, because tested agent code moved out; agentctl is at 67%. What remains is orchestration, config loading, image-metadata merge, environment setup, SSH config and `Lock`.

**Step 6 (thin CLI and renames): done.**
- **Status and list data come from the service.** `Service.Status()` and `Service.List()` return data and the CLI only renders it. `runStatus` (cyclomatic 35) and the list grouping logic are gone from the CLI, and status now lists features in sorted order. Golden tests in `internal/cli/testdata` pin the status (normal, detailed, absent) and list output; `Service.List` has a fake-engine test.
- **Composition root:** `cli/context.go` holds `commandContext` (Ctrl-C aware) and `newService`. No command builds its own service any more, and none uses `context.Background()`, so Ctrl-C cancels `lock`, `doctor`, `debug`, `clean` and `ssh info` too. `lock` and `config` no longer require a running Docker daemon.
- **Identity:** `service.Identifiers` and `devcontainer.DevContainerID` became one `devcontainer.Identity{WorkspaceID, Name, ResourceName, SSHHost}`, read through `Service.Identity()`. "ProjectName", which meant two things, is now `ResourceName`; status still labels it "Project" (scripts and tests parse that line). When the config is unreadable, the resource name now comes from the directory name instead of being empty.
- **Get prefixes dropped:** `ImageID`/`ImageLabels`/`Logs` (Engine), `StateManager`, `State`/`StateWithHash`, `ComposeProjectOr`/`ConfigDirOr`, `lockfile.Path`, `dcxembed.Binary`, `Feature.EnvVars`/`OptionValue`, `DerivedImageStats`, `cfg.DcxCustomizations()` (now a method), `selinux.CurrentMode` and `features.CollectSecurityRequirements` (the last two avoid clashing with type names). `GetDockerComposeFiles` is gone (use the field), and `GetPortAttribute` is now `PortAttributeFor`. The only remaining `Get*` functions are e2e helpers.
- **Not done:** per-command option structs. Cobra flags are still package globals, but only `context.go` reads the workspace and config ones.

## Where this leaves the codebase

All six steps are done. Compared with this audit's starting point:
- the confirmed bugs (two root injections among them) are fixed, with regression tests;
- about 1,500 lines of dead code are gone;
- only `container/docker.go` runs `docker`, enforced by lint;
- `up` makes one decision, and stop/restart and the agent each have one owner;
- the CLI renders service data and has golden tests;
- the vocabulary matches what the code does.

Still open:
- image metadata for Dockerfile and compose bases;
- `waitFor` (everything runs in the foreground);
- a friendly "not running" message for plain `ssh`;
- `cli` and `container` coverage (thin rendering and Docker CLI glue, best covered by the e2e suite).

## 5. Recommended plan

Each step ships on its own, in this order:

1. **Bugs (1–2 days).**
   - B1/B2: build file contents in Go and pipe them on stdin to a fixed script (`WriteFileInContainer`). Make `/etc/environment` a managed block that is rewritten on each `up`, not appended once.
   - B3: remove the probe cache, or keep it in one file.
   - B4: fix lock verify/frozen and merge the branches.
   - B5: extract into a temp dir and rename into place; remove the "no digest means trusted" path.
   - B6–B12, each with a regression test.
2. **Aggressive deletion pass (1 day).** Everything under "Delete" in section 3, plus the tautological tests, the history comments and the text fixes. Mostly negative lines.
3. **Complete the Engine (2–3 days).** Section 2.1. Then add Fake tests for build, deploy and env, and a depguard rule allowing `os/exec` only in `container` and `agent`.
4. **One reconcile decision (1–2 days).** Section 2.2: `Service.Stop`/`Restart`, `stepContainer` on `DeterminePlanAction`, and `UnifiedRuntime` → `Provisioner`.
5. **Extract agent provisioning (2 days).** Section 2.3: `internal/agentctl`, identifiers computed once, `ssh/server` → `internal/agent`.
6. **Thin CLI and renames (2 days).** Section 2.4, plus the remaining renames from section 3 in one mechanical commit, with golden tests for `plan`/`status` output.

**Total:** about two weeks. After step 3, every step can be unit-tested against `containertest.Fake`.
