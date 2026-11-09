# Plugin anatomy

What `pvtr generate-plugin` emits, how `main.go` should be wired, and how a
plugin is run. Verified against pvtr 0.24.1, privateer-sdk v1.34.1, and
go-gemara v0.11.0. `generate-plugin` clones the templates repo's `main` on
every run, so the scaffold changes without a pvtr release.

## Generated tree

```text
<plugin-dir>/
  main.go                          # orchestrator wiring; see below
  go.mod                           # pins privateer-sdk and go-gemara; no go.sum until tidy
  data/
    data_collection.go             # Payload + Loader
    catalogs/catalog_<id>_<ver>.yaml  # the catalog, embedded at build time
  evaluation_plans/
    evaluation_plans.go            # TypedStep, and Suite_<id>: map[requirement id] -> step chain
    reusable_steps/steps.go        # NotImplemented
  example-config.yml               # copy to config.yml to run
  Makefile                         # build, test, vet, tidy, cross-platform release builds
  README.md, TESTING.md
  .github/, .goreleaser.yaml       # CI and release, copied verbatim from the templates
```

Every requirement id in the catalog appears in `Suite_<id>` bound to
`reusable_steps.NotImplemented`. Replacing those is the job.

## `main.go` wiring

The scaffold's `main.go` is the reference; this section does not copy it. It
registers the typed `Suite_<id>` map through `pluginkit.AddEvaluationSuiteTyped`
(the SDK asserts the payload type once and keeps each step's function name in
the results) and leaves three stubs to fill:

```go
Publisher: "acme",        // grc.store namespace; required only to publish
License:   "Apache-2.0",  // SPDX expression; required only to publish

orchestrator.AddTargetBuilder(func(c *config.Config) gemara.Resource {
    // Identify what was evaluated, e.g. owner/repo from c.Vars. Empty fields
    // fall back to the service name, so the unfilled stub is harmless.
    return gemara.Resource{Id: ..., Name: ..., Type: gemara.Software, Uri: ...}
})
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

### Older scaffolds

A scaffold containing `VerifyPayload`, or without the stubs above, came from
older templates. Upgrade it by diffing against a scratch scaffold (SKILL.md,
"Generate, then copy") rather than from a list here. Expect the diff to show
typed registration in place of `AddEvaluationSuite`, and a
`NewPluginCommands` call that passed `VersionPostfix` where the SDK takes
`(pluginName, buildVersion, buildGitCommitHash, buildTime, orchestrator)`.
Convert to typed registration before writing steps, so no step needs a payload
guard.

## Running

Three ways, in the order to use them:

| Mode | Command | Use for |
| --- | --- | --- |
| Unit tests | `go test ./...` | Step logic against payload literals. Fast, no credentials. |
| Plugin debug | `./<bin> debug --target <name> -l debug` | The loader against a real target, without pvtr. |
| Full run | `pvtr install --local ./<bin>`, then `pvtr run` | What users see. Services in `config.yml`, with `plugin: local/<bin>`. |

Config shape (`example-config.yml` is the template):

```yaml
loglevel: info
write-directory: evaluation_results
output: yaml            # yaml | json | sarif | gemara
services:
  my-target:            # --target picks this entry
    plugin: local/<PluginName>  # as installed; <namespace>/<PluginName> from grc.store
    policy:
      catalogs: ["<catalog id>"]
      applicability: [<category>]
    vars:
      owner: example
      repo: thing
```

`applicability` must be non-empty, and each requirement in the catalog must
declare an applicability that matches it. A requirement with none is reported
`Unknown` with "expected all AssessmentLog fields to have a value" and its
steps never run. The scaffold's `example-config.yml` lists every category the
catalog declares.

Results land in `<write-directory>/<target>/<target>.<output>`. Under `pvtr run`
the target name is lowercased (`myService1` becomes `myservice1`), because
viper lowercases config keys; `debug` keeps `--target` as typed. On a
case-sensitive filesystem, read the `pvtr run` log from the lowercase path.

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
