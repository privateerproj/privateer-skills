---
name: privateer-plugin
description: Build or extend a Privateer plugin — a Go binary that evaluates a target against a Gemara Layer 2 control catalog and emits a Gemara EvaluationLog through `pvtr run`. Use when the user says "write a Privateer plugin", "pvtr plugin", "implement the assessment steps for <catalog>", "add a step for <requirement id>", "wire the data loader", or wants a plugin that compiles, tests, and runs idiomatically against the current privateer-sdk. Not for authoring or evolving catalogs, reading existing evaluation logs, or unrelated Go work.
---

# privateer-plugin

Produces a plugin that `pvtr run` can execute, where every assessment
requirement in the catalog has a registered, tested step chain, and the emitted
EvaluationLog reflects real observations.

## Source of truth

This skill carries conventions, not copies of code. Where they disagree with
the SDK you have installed, the SDK wins; say so in the report.

1. `pvtr generate-plugin` output — the layout every plugin starts from.
2. `go doc github.com/privateerproj/privateer-sdk/pluginkit` — the orchestrator API.
3. [pvtr-github-repo-scanner](https://github.com/ossf/pvtr-github-repo-scanner)
   — the maintained reference plugin. When unsure how something is done, read it.

References, loaded on demand:

- `references/plugin-anatomy.md` — generated tree, `main.go` wiring, config,
  run modes, exit codes, known template lag.
- `references/steps.md` — step signature, result semantics, chain ordering,
  the halt rule, testing.
- `references/loader.md` — payload and DataLoader conventions.
- `scripts/README.md` — `sync` and `run-summary` inspection helpers.

## Preflight

```sh
pvtr version            # need >= 0.23.0; else: brew install privateerproj/tap/pvtr
go version              # need the Go line in the generated go.mod (1.26+)
```

A plugin needs a Gemara Layer 2 `ControlCatalog` (YAML or JSON, file or URL).
If the user has none, stop and ask for one. Do not synthesize a catalog; that is
a different task with its own review needs. A minimal valid catalog is in
`references/plugin-anatomy.md` for smoke tests only.

## Workflow

Steps 1 and 2 are for a new plugin. For an existing plugin, start at step 3 and
never run `generate-plugin` again: it overwrites every hand-edited file.

1. **Scaffold, then prove it compiles.**

   ```sh
   pvtr generate-plugin \
     --source-path  <catalog.yaml> \
     --service-name <Service> \
     --organization <github-org> \
     --output-dir   <plugin-dir>
   cd <plugin-dir> && go mod tidy && go build ./... && go vet ./...
   ```

   `generate-plugin` exits non-zero on template failure since pvtr 0.20.3, but
   still run the build; a scaffold that does not compile is not a starting point.

2. **Wire `main.go`** per `references/plugin-anatomy.md`: fix the
   `NewPluginCommands` argument order, add a target builder, and register
   steps with `pluginkit.AddEvaluationSuiteTypedForAllCatalogs` and a
   plugin-local `TypedStep` type. Set `Publisher` and `License` when the plugin
   will be published.

3. **Write the loader** per `references/loader.md`. One `data.Payload`, one
   `data.Loader`. All I/O lives here.

4. **Write the steps** per `references/steps.md`. One package per control
   family under `evaluation_plans/`, cross-cutting checks in `reusable_steps`,
   and one map keyed by requirement id that names the chain for each.

5. **Test every step** with a table of `data.Payload` literals. No network in
   tests; fake at the payload, or at an injected HTTP client for loader tests.

6. **Sync check.** Every live requirement id in the catalog must map to a chain,
   and no chain may name an id the catalog lacks:

   ```sh
   (cd <skill-dir>/scripts && go run . sync \
       --catalog <plugin-dir>/data/catalogs/<catalog>.yaml \
       --plugin  <plugin-dir> --strict)
   ```

   `missing_in_plugin` → back to step 4. `orphaned_in_plugin` → ask; an orphan
   is usually a typo in the id, sometimes a retired control kept for visibility.

7. **Run it.** First the plugin alone, then under `pvtr`:

   ```sh
   cp example-config.yml config.yml      # fill vars for a real target
   go build -o <bin> . && ./<bin> debug --target <name> -l debug
   (cd <skill-dir>/scripts && go run . run-summary --path evaluation_results/<name>/<name>.yaml)
   ```

   Then install the binary into `~/.privateer/bin/`, put the same `services:`
   block in a `config.yml`, and run `pvtr run`. Read the exit code with the
   table in `references/plugin-anatomy.md`; `1` means assessments failed,
   which is data, not a broken plugin.

8. **Done** when all of these hold. Report the EvaluationLog path and the
   `totals` map from `run-summary`; on any miss, paste the failing JSON.

   - [ ] `go build ./... && go vet ./... && go test ./...` pass.
   - [ ] `sync --strict` exits 0.
   - [ ] `run-summary` shows every live requirement id with `steps_executed >= 1`
         and a result other than `Unknown` or `Not Run`, and `unknown_ids: []`.
   - [ ] `pvtr run` exit code is 0 or 1.

## Guardrails

- Never re-run `generate-plugin` over an existing plugin directory.
- Never add a `replace` directive to `go.mod`. If the plugin needs unreleased
  SDK code, say so; do not mask it.
- Steps never touch the network or filesystem. If a step needs data the payload
  lacks, extend the loader.
- Never return `Passed` without a positive observation. Missing or unreadable
  data is `NeedsReview` (or `Unknown` for a loader defect), not `Passed` and
  not `Failed`.
- Never infer success from `pvtr run`'s exit code alone. Parse the log.
- Do not run `pvtr install`, `pvtr publish`, or `brew upgrade` on the user's
  behalf. Surface the command and let them decide.
- If the installed SDK's API differs from these references, follow `go doc`
  and note the drift in the report so the skill can be updated.
