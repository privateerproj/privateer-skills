# scripts

Inspection helpers the skill shells out to instead of grepping. One Go
program, two subcommands, JSON on stdout. Inside a Go workspace (a `go.work`
above this directory), prefix the commands with `GOWORK=off`.

```sh
(cd scripts && go run . sync --catalog <plugin-dir>/data/catalogs --plugin <plugin-dir> [--strict])
(cd scripts && go run . run-summary --path <evaluation-log.yaml>)
```

## sync

Walks the plugin's Go source for composite literals typed `map[string][]<...Step>`
(the scaffold's `TypedStep` map, and the untyped `gemara.AssessmentStep` map
of older scaffolds, both match) and diffs their string-literal keys against every
non-Retired requirement id in the catalog. `--catalog` takes one file or a
directory; a directory's `*.yaml`/`*.yml` catalogs are checked together, since
a multi-catalog plugin registers all of their ids.

- `missing_in_plugin` — live requirement with no chain. Implement it.
- `orphaned_in_plugin` — chain whose id the catalog does not have at all.
  Usually a typo, or an id dropped from a new catalog version. An id the
  catalog marks `Retired` may stay bound (to a `Retired` step) and is not an
  orphan.
- `duplicate_ids_in_catalog`, `dynamic_keys` — cannot be verified statically.
- `synced` — true when all four are empty. `--strict` exits 1 otherwise.

## run-summary

Parses a `pvtr run` EvaluationLog (YAML) and emits per-requirement result,
`steps_executed`, and message, plus `totals` by result and `unknown_ids` for
assessment logs missing a requirement id.
