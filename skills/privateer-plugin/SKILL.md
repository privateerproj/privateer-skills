---
name: privateer-plugin
description: Build or extend a Privateer plugin — a Go binary that evaluates a target against a Gemara Layer 2 control catalog and emits a Gemara EvaluationLog through `pvtr run`. Use when the user says "write a Privateer plugin", "pvtr plugin", "implement the assessment steps for <catalog>", "add a step for <requirement id>", "wire the data loader", or wants a plugin that compiles, tests, and runs idiomatically against the current privateer-sdk. Also covers updating a plugin when its catalog gains a new version. Not for designing a catalog's controls, reading existing evaluation logs, or unrelated Go work.
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
  stubs to fill, run modes, exit codes, upgrading older scaffolds.
- `references/steps.md` — step signature, result semantics, chain ordering,
  the halt rule, testing.
- `references/loader.md` — payload and DataLoader conventions.
- `scripts/README.md` — `sync` and `run-summary` inspection helpers.

## Preflight

```sh
pvtr version            # need >= 0.24.2 (a source build prints "edge"); else: brew install (or upgrade) privateerproj/tap/pvtr
go version              # need at least the go line in the generated go.mod; Go fetches it unless GOTOOLCHAIN=local
```

A plugin needs a Gemara Layer 2 `ControlCatalog` (YAML or JSON, file or URL).
If the user has none:

- If they state the requirements themselves (what each must check), write them
  into a catalog shaped like the minimal one in `references/plugin-anatomy.md`:
  keep their wording, give every requirement `applicability`, and set
  `metadata.version`. Show the YAML and get a yes before scaffolding.
- Otherwise stop and ask. Do not invent requirements or wording; designing
  controls is a different task with its own review needs.

Run the `scripts/` helpers with `GOWORK=off` when a `go.work` sits above the
skill directory, and pass them absolute paths: each command `cd`s into
`scripts/` first.

## Workflow

Steps 1 and 2 are for a new plugin. For an existing plugin whose catalog
changed, start at "Catalog changed" below; otherwise start at step 3. Never run
`generate-plugin` over an existing plugin: it overwrites every hand-edited file.
Anything the generator can write, take from a scratch scaffold instead of
typing it (see "Generate, then copy").

1. **Scaffold, then prove it compiles.**

   ```sh
   pvtr generate-plugin \
     --source-path  <catalog.yaml> \
     --service-name <Service> \
     --organization <github-org> \
     --output-dir   <plugin-dir>
   cd <plugin-dir> && go mod tidy && go build ./... && go vet ./...
   go list -m github.com/privateerproj/privateer-sdk   # need >= v1.34.2
   ```

   If that prints an older SDK, raise it before writing code:
   `go get github.com/privateerproj/privateer-sdk@v1.34.2 && go mod tidy`.
   Older SDKs write every config var, tokens included, into results produced
   with `--include-payload`. Never lower a newer pin.

   `--service-name` becomes the plugin name, the module path's last element,
   the binary name and the installed `local/<name>`, so use a short lowercase
   identifier. `generate-plugin` exits non-zero on template failure since pvtr
   0.20.3, but still run the build; a scaffold that does not compile is not a
   starting point. Keep the code `gofmt`-clean; the generated CI lints it.

2. **Fill the `main.go` stubs** per `references/plugin-anatomy.md`: the
   `AddTargetBuilder` body, and `Publisher` and `License` when the plugin will
   be published. The scaffold already registers a plugin-local `TypedStep` map
   through `pluginkit.AddEvaluationSuiteTyped`. A scaffold with `VerifyPayload`
   or without the stubs is older; upgrade it per "Generate, then copy".

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
       --catalog <plugin-dir>/data/catalogs \
       --plugin  <plugin-dir> --strict)
   ```

   Pass the directory, not one file: a plugin registers every embedded
   catalog's ids, so checking one catalog reports the others' ids as orphans.
   Sync proves coverage, not implementation: a fresh scaffold passes with every
   id still on `NotImplemented`. `missing_in_plugin` → take the entries from a scratch scaffold ("Generate,
   then copy"), then back to step 4. `orphaned_in_plugin` → ask; an orphan is
   usually a typo in the id, sometimes a retired control kept for visibility.

7. **Run it.** First the plugin alone, then under `pvtr`:

   ```sh
   cp example-config.yml config.yml      # replace the myvar placeholders with the vars your loader reads; keep applicability non-empty
   go build -o <bin> . && ./<bin> debug --target <name> -l debug
   log="$PWD/evaluation_results/<name>/<name>.yaml"   # absolute: the next line cds away
   (cd <skill-dir>/scripts && go run . run-summary --path "$log")
   ```

   `<name>` is the service key under `services:` in `config.yml`, not the
   plugin name. `debug` exits 0 whatever the results; only the log counts. Then register the
   binary with pvtr and run it the way users will:

   ```sh
   pvtr install --local ./<bin>          # records local/<bin> in pvtr's plugin manifest
   pvtr run                              # config.yml must say plugin: local/<bin>
   ```

   Copying the binary into `~/.privateer/bin` is not enough; pvtr resolves
   `plugin:` through the manifest. With `Publisher` empty, the install prints
   a "declares no grc.store Publisher" warning followed by the plugin's whole
   `publish-manifest` usage text, skips catalog install, and exits 0. It looks
   like a crash; it is not. That is harmless for embedded catalogs (the scaffold); a plugin
   that declares grc.store catalogs with `AddCatalogs` must set `Publisher`
   first, or `pvtr run` will not find them. Read the
   `pvtr run` exit code with the table in `references/plugin-anatomy.md`; `1`
   means assessments failed, which is data, not a broken plugin. `pvtr run`
   writes to a lowercased `evaluation_results/<name>/` path; see
   `references/plugin-anatomy.md`. The console line
   `> <suite>: N Passed, N Warnings, N Failed, N Possible` counts controls, not
   requirements: `Warnings` is every control neither passed nor failed (Not
   Applicable included, which can log at ERROR level) and `Possible` is the
   control count. Trust `run-summary`, not that line.

8. **Done** when all of these hold. Report the EvaluationLog path and the
   `totals` map from `run-summary`; on any miss, paste the failing JSON.

   - [ ] `go build ./... && go vet ./... && go test ./...` pass.
   - [ ] `sync --strict` exits 0.
   - [ ] `run-summary` shows every live requirement id with `steps_executed >= 1`
         and a result other than `Unknown` or `Not Run`, and `unknown_ids: []`.
   - [ ] `pvtr run` exit code is 0 or 1.

## Generate, then copy

For an existing plugin, run the generator into a scratch directory with the
plugin's own `--service-name` and `--organization`, then copy pieces out:

```sh
scratch=$(mktemp -d)
pvtr generate-plugin --source-path <catalog> --service-name <Service> \
  --organization <github-org> --output-dir "$scratch"
```

| Task | Take from `$scratch` |
| --- | --- |
| Add a catalog | `data/catalogs/catalog_*.yaml` into the plugin's `data/catalogs/`; the `Suite_<id>` block from `evaluation_plans/evaluation_plans.go`; the `AddEvaluationSuiteTyped` call from `main.go`; the `applicability` list from `example-config.yml` |
| New catalog version | Follow "Catalog changed" below |
| `missing_in_plugin` | Only the missing ids' entries from `Suite_<id>`, merged into the existing map |
| Upgrade an older scaffold | `diff -r "$scratch" <plugin-dir>`, then port the wiring the plugin lacks; keep its steps, loader and catalogs |

Copy the generated catalog file rather than the source: the generator
re-serializes it and names it the way the plugin embeds it. Then rerun sync.

### Catalog changed

A binary embeds one version per catalog id; two files with the same id stop
the plugin at startup with `duplicate catalog id found`. Sync compares ids
only, so a requirement whose text changed under the same id is invisible to it.

1. Scaffold the new version into `$scratch` as above, then see which ids were
   added or dropped before touching the plugin:

   ```sh
   (cd <skill-dir>/scripts && go run . sync \
       --catalog "$scratch"/data/catalogs --plugin <plugin-dir>)
   ```

2. Diff the requirement text of the ids both versions share:

   ```sh
   diff <plugin-dir>/data/catalogs/catalog_<id>_<old>.yaml \
        "$scratch"/data/catalogs/catalog_<id>_<new>.yaml
   ```

   For every changed `text`, reread the bound step against the new wording and
   change the step and its tests where the meaning moved; extend the loader
   first if the payload lacks the data the new wording needs. A step that
   passes sync and tests can still enforce the old rule. A fixture that the
   old rule passes and the new rule fails proves the change.
3. Copy the new catalog file in, then `git rm` the old one (in that order:
   removing the only file first deletes `data/catalogs/`). The generator
   re-serializes the catalog and may drop or rename fields such as `title`;
   that is expected.
4. Merge the added ids' entries from `$scratch`'s `Suite_<id>`. A requirement
   now in state `Retired` stays in the map, bound to a `Retired` step
   (`references/steps.md`); sync accepts it. An id gone from the catalog
   entirely shows up as `orphaned_in_plugin`; ask before deleting it.
5. Back to step 4 of the workflow for the new steps, then steps 5 to 8.
   Rerun `pvtr install --local` after rebuilding: `pvtr run` runs the
   installed copy, not the binary in the plugin directory.

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
- `pvtr install --local` on the plugin you just built is part of step 7. Do not
  install from grc.store, `pvtr publish`, or `brew upgrade` on the user's
  behalf; surface the command and let them decide.
- If the installed SDK's API differs from these references, follow `go doc`
  and note the drift in the report so the skill can be updated.
