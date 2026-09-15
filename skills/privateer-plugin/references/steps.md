# Assessment steps

A step is a pure function of the payload. It observes, it does not fetch.

```go
// One per plugin, in evaluation_plans/typed_step.go.
type TypedStep func(data.Payload) (gemara.Result, string, gemara.ConfidenceLevel)
```

## Results

`gemara.Result` values and when to return each:

| Result | Return when | Halts the chain |
| --- | --- | --- |
| `Passed` | You observed the requirement being met. | no |
| `Failed` | You observed it not being met. | yes |
| `NotApplicable` | The requirement does not apply to this target (not a code repo, no releases yet). | yes |
| `NeedsReview` | The data needed is absent, ambiguous, or only a heuristic could judge it. | no |
| `Unknown` | The payload was unusable. Almost always a loader bug. | no |
| `NotRun` | Placeholder only: `NotImplemented` and `Retired` steps. | no |

The halt rule is go-gemara's (`AssessmentLog.Run`): the chain stops on the
first `Failed` or `NotApplicable`; every other result is folded into the
aggregate and the next step runs. The recorded message and confidence are
always the last step's, so a late `NeedsReview` after an early `Passed`
reports as review-needed, which is correct.

Two consequences:

- **Order chains from "is the data there" to "is the value acceptable".** A
  scope guard (`IsCodeRepo`, `IsActive`) goes first so an inapplicable target
  halts before a substantive step fails it for the wrong reason.
- **Absent data is never `Failed`.** "No SECURITY.md found" is `Failed` only if
  the loader positively listed the tree; "could not list the tree" is
  `NeedsReview` with the reason.

`gemara.ConfidenceLevel`: `High` for a direct observation, `Medium` for a
derived signal, `Low` for a heuristic or AI-assisted judgment, `Undetermined`
(zero value) when the payload was unusable. The scanner leaves confidence at
zero for deterministic steps; that is acceptable, but set it where a reader
would otherwise ask "how sure is this".

## Messages are evidence

The message is what a reviewer reads instead of your code. Include the value
observed, not just the verdict:

```go
return gemara.Failed, fmt.Sprintf("Branch protection not enforced: required reviews = %d", n), gemara.High
```

For richer evidence, embed `gemara.EvidenceCollector` in the payload and call
`payload.AddEvidence(...)` from a step; the SDK copies it into the log after
each step.

## Layout and naming

```text
evaluation_plans/
  typed_step.go                # TypedStep + AllSteps() when merging several maps
  evaluation_plans.go          # the id -> chain map
  reusable_steps/steps.go      # cross-cutting: NotImplemented, Retired, IsCodeRepo, HasSecurityInsightsFile
  <family>/steps.go            # one package per control family (access_control, legal, ...)
  <family>/steps_test.go
```

- Step names are exported verb phrases that read as the claim they check:
  `BranchProtectionPreventsDeletion`, `ReleasesAreSignedOrAttested`.
- Keep one implementation per requirement id across catalog versions. If a
  requirement's meaning changes, the catalog gives it a new id; bind that.
- A retired id stays in the map bound to `reusable_steps.Retired` with a
  comment saying which version retired it. Do not delete it.
- Placeholders return `NotRun`:

  ```go
  func NotImplemented(p data.Payload) (gemara.Result, string, gemara.ConfidenceLevel) {
      return gemara.NotRun, "Not implemented", 0
  }
  ```

- Helpers that are not steps (`HasPublishedRelease`, `ReleaseLabel`) live
  beside the steps but return plain Go values, so steps stay one-line readable.

## Anti-patterns

- One step that does everything. Split so `steps-executed` shows where it stopped.
- Network or filesystem access in a step. That belongs in the loader; see
  `loader.md` for lazy fetching if the data is expensive.
- A payload type assertion in every step. The typed registration does it once.
- Wrapping a step in a closure to adapt it. That erases its name from the log.
- `Passed` on missing data. Silent green is the worst outcome for an evaluator.
- Reading `payload.Config.Vars` in a step to decide behavior. Resolve config in
  the loader and put the resolved value in the payload.

## Testing

Table-driven, one `t.Run` per case, payload built as a literal. The scanner's
`osps/*/steps_test.go` files are the pattern.

```go
func TestHasContributionGuide(t *testing.T) {
    tests := []struct {
        name    string
        payload data.Payload
        want    gemara.Result
        wantMsg string
    }{
        {"present", data.Payload{Insights: insightsWith("CONTRIBUTING.md")}, gemara.Passed, "Contribution guide found"},
        {"absent", data.Payload{Insights: insightsWith()}, gemara.Failed, "No contribution guide listed"},
        {"insights unreadable", data.Payload{InsightsError: true}, gemara.NeedsReview, "Security Insights could not be parsed"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, msg, _ := HasContributionGuide(tt.payload)
            if got != tt.want || msg != tt.wantMsg {
                t.Errorf("got (%v, %q), want (%v, %q)", got, msg, tt.want, tt.wantMsg)
            }
        })
    }
}
```

Every step gets at least the pass case, the fail case, and the absent-data
case. If a step reads through a lazily fetched field, inject the fetcher (the
scanner passes `makeApiCall` as an optional argument) and use `httptest` rather
than the network.
