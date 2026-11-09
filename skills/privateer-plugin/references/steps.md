# Assessment steps

A step is a pure function of the payload. It observes, it does not fetch.

```go
// One per plugin; the scaffold declares it in evaluation_plans/evaluation_plans.go.
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

- **Put scope guards first.** A guard (`IsCodeRepo`, `HasDeployments`)
  returns `NotApplicable` so an inapplicable target halts before a substantive
  step fails it for the wrong reason.
- **A `NeedsReview` guard protects nothing.** It does not halt, so the next
  step still runs on the missing data. Every step that reads a field checks
  that field's `...Error` itself; a shared helper keeps that to one line.
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

Structured evidence is optional; a message carrying the observed value is
the floor. Record evidence when a reviewer needs the raw data (a list of
offending files, a header value) rather than a sentence. Embed
`*gemara.EvidenceCollector` in the payload (a pointer, set in the loader;
`loader.md`, "Evidence") and add entries from a step; the SDK copies them into
the log after each step:

```go
func record(p data.Payload, kind, desc string, observed any) {
    if p.EvidenceCollector == nil { // payload literals in tests
        return
    }
    p.AddEvidence(gemara.Evidence{
        Id:          kind,                          // unique within the step
        Type:        gemara.EvidenceType(kind),     // free-form category; go-gemara defines no constants
        CollectedAt: gemara.Datetime(p.CollectedAt), // RFC 3339, stamped once by the loader
        Payload:     observed,                      // the raw value; payload or Source is required
        Description: desc,
    })
}
```

Leave `Source` empty: it cites an artifact by mapping reference, which an
evaluation plugin rarely has.

## Layout and naming

```text
evaluation_plans/
  evaluation_plans.go          # TypedStep and the id -> chain map (split out AllSteps() when merging several maps)
  reusable_steps/steps.go      # cross-cutting: NotImplemented, Retired, IsCodeRepo, HasSecurityInsightsFile
  <family>/steps.go            # one package per control family (access_control, legal, ...)
  <family>/steps_test.go
```

- Step names are exported verb phrases that read as the claim they check:
  `BranchProtectionPreventsDeletion`, `ReleasesAreSignedOrAttested`.
- Keep one implementation per requirement id. A catalog should give a
  requirement a new id when its meaning changes, but not every catalog does,
  and sync cannot tell; diff the text when the catalog changes (SKILL.md,
  "Catalog changed").
- A retired id stays in the map bound to `reusable_steps.Retired` with a
  comment saying which version retired it. Do not delete it. The scaffold has
  no `Retired`; add it beside `NotImplemented`, returning `NotRun` and
  `"Retired in <version>"`.
- Placeholders return `NotRun`:

  ```go
  func NotImplemented(p data.Payload) (gemara.Result, string, gemara.ConfidenceLevel) {
      return gemara.NotRun, "Not implemented", gemara.Undetermined
  }
  ```

- Helpers that are not steps (`HasPublishedRelease`, `ReleaseLabel`) live
  beside the steps but return plain Go values, so steps stay one-line readable.

## Anti-patterns

- One step that does everything. Split so `steps-executed` shows where it
  stopped. The halt rule cuts both ways: after a `Failed` step, later steps in
  the chain never run, so their findings are lost. Split by precondition
  (data present, then value acceptable); keep independent failure modes of one
  requirement in one step that lists every violation.
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
case. Call the step function directly; to assert recorded evidence, build the
payload with a collector and read `p.GetEvidence()`. A family package and its tests must not
import `evaluation_plans` (it imports them: an import cycle), so spell out
`func(data.Payload) (gemara.Result, string, gemara.ConfidenceLevel)` if a test
table needs the type. If a step reads through a lazily fetched field, inject the fetcher (the
scanner passes `makeApiCall` as an optional argument) and use `httptest` rather
than the network.
