# Plugin anatomy

What `pvtr generate-plugin` emits, how `main.go` should be wired, and how a
plugin is run. Verified against pvtr 0.23.0, privateer-sdk v1.33.x, and
go-gemara v0.10.0.

## Generated tree

```text
<plugin-dir>/
  main.go                          # orchestrator wiring; see below
  go.mod                           # pins privateer-sdk and go-gemara; no go.sum until tidy
  data/
    data_collection.go             # Payload + Loader
    catalogs/catalog_<id>_<ver>.yaml  # the catalog, embedded at build time
  evaluation_plans/
    evaluation_plans.go            # Suite_<id>: map[requirement id] -> step chain
    reusable_steps/steps.go        # VerifyPayload, NotImplemented
  example-config.yml               # copy to config.yml to run
  Makefile                         # build, test, vet, tidy
  .github/, .goreleaser.yaml       # CI and release, copied verbatim from the templates
```

Every requirement id in the catalog appears in `Suite_<id>` bound to
`reusable_steps.NotImplemented`. Replacing those is the job.

## `main.go` wiring

The scaffold uses untyped `gemara.AssessmentStep` and a `VerifyPayload` cast in
every step. The current idiom is typed steps registered through the SDK, which
does the cast once and keeps each step's function name in the results. Rewrite
the scaffold's registration to this shape:

```go
// evaluation_plans/typed_step.go
type TypedStep func(data.Payload) (gemara.Result, string, gemara.ConfidenceLevel)

// evaluation_plans/evaluation_plans.go
var Suite = map[string][]TypedStep{
    "ID.C01.TR01": {reusable_steps.HasThing, family.ThingIsConfiguredRight},
}
```

```go
// main.go
orchestrator := &pluginkit.EvaluationOrchestrator{
    PluginName:    PluginName,
    PluginVersion: Version,
    PluginUri:     "https://github.com/<org>/<repo>",
    Publisher:     "<grc.store namespace>", // required only to publish
    License:       "Apache-2.0",            // SPDX expression; required only to publish
}
orchestrator.AddLoader(data.Loader)
orchestrator.AddTargetBuilder(func(c *config.Config) gemara.Resource {
    // Identify what was evaluated from the config vars, e.g. owner/repo.
    return gemara.Resource{Id: ..., Name: ..., Type: gemara.Software, Uri: ...}
})
if err := orchestrator.AddReferenceCatalogs(dataDir, files); err != nil { ... }
orchestrator.AddRequiredVars(RequiredVars)
if err := pluginkit.AddEvaluationSuiteTypedForAllCatalogs(orchestrator, nil, evaluation_plans.Suite); err != nil { ... }

runCmd := command.NewPluginCommands(PluginName, Version, GitCommitHash, BuiltAt, orchestrator)
```

Rules that follow from the SDK:

- Register every `TypedStep` map through `AddEvaluationSuiteTyped` or
  `...ForAllCatalogs`. Wrapping a step in a local closure erases its name from
  the log and the benchmark report.
- All steps in one registration share one payload type. A second payload type
  needs a second registration call.
- `ForAllCatalogs` is right when one implementation per id holds across catalog
  versions. Ids a given catalog does not declare are simply never run.
- `RequiredVars` lists config `vars` the loader cannot work without. The SDK
  refuses to run when one is missing, so a step never sees a half-built payload.
- Exit with `shared.InternalError` on wiring failures, not a bare `1`.

### Known template lag (as of plugin-generator-templates at SDK v1.33.0)

- The scaffold calls `NewPluginCommands(PluginName, Version, VersionPostfix, GitCommitHash, &orchestrator)`.
  The SDK signature is `(pluginName, buildVersion, buildGitCommitHash, buildTime, orchestrator)`.
  It compiles because every argument is a string, but the commit hash and build
  time land in the wrong fields. Fix the call order as shown above.
- The scaffold registers an untyped map with `AddEvaluationSuite`. Convert to
  typed registration before writing steps, so no step needs a payload guard.

## Running

Three ways, in the order to use them:

| Mode | Command | Use for |
| --- | --- | --- |
| Unit tests | `go test ./...` | Step logic against payload literals. Fast, no credentials. |
| Plugin debug | `./<bin> debug --target <name> -l debug` | The loader against a real target, without pvtr. |
| Full run | `pvtr run` | What users see. Binary in `~/.privateer/bin/`, services in `config.yml`. |

Config shape (`example-config.yml` is the template):

```yaml
loglevel: info
write-directory: evaluation_results
output: yaml            # yaml | json | sarif | gemara
services:
  my-target:            # --target picks this entry
    plugin: <PluginName>
    policy:
      catalogs: ["<catalog id>"]
      applicability: [<category>]
    vars:
      owner: example
      repo: thing
```

Results land in `<write-directory>/<target>/<target>.<output>`.

### Exit codes (`privateer-sdk/shared/exitcodes.go`)

| Code | Name | Meaning for the skill |
| --- | --- | --- |
| 0 | TestPass | Ran; everything passed. |
| 1 | TestFail | Ran; some assessments failed. Success from the plugin's point of view. |
| 2 | Aborted | Stop and surface. |
| 3 | InternalError | Plugin or wiring defect. Stop and surface. |
| 4 | BadUsage | Config or flag error. Fix and rerun. |
| 5 | NoTests | No steps ran; usually a catalog id or applicability mismatch. |

Never decide "done" from 0 or 1 alone; parse the log with `scripts run-summary`.

## Minimal smoke-test catalog

For proving the toolchain, not for shipping. Mirrors the templates' own CI.

```yaml
title: Smoke Test Catalog
metadata:
  id: smoke-test
  type: ControlCatalog
  gemara-version: v1.0.0
  description: Minimal catalog used to verify a plugin compiles and runs.
  author:
    id: example
    name: Example
    type: Human
controls:
  - id: SMOKE.C01
    family: smoke
    title: Smoke Control
    objective: Verify that the plugin compiles.
    assessment-requirements:
      - id: SMOKE.C01.TR01
        text: The plugin MUST compile.
        applicability: [default]
```
