# Payload and loader

All I/O happens once, up front, in the loader. Steps then read a value.

```go
// privateer-sdk/pluginkit
type DataLoader func(*config.Config) (any, error)
```

The loader returns the payload; the SDK stores it and passes it to every step,
and writes it into the EvaluationLog under `payload` when the user asks for it.

## Payload

```go
// data/data_collection.go (or data/payload.go as it grows)
type Payload struct {
    *RestData                          // pointer fields keep the by-value payload cheap to copy
    *pluginkit.APICallCounter          // enables API counting in `pvtr benchmark`
    *gemara.EvidenceCollector          // embedded pointer, set in the loader; see Evidence below
    Owner     string                   // resolved from cfg once, here, not in steps
    Repo      string
    IsCodeRepo bool
    ReleasesError string               // "could not observe", distinct from "observed none"; a string, since an error field serializes as {}
    CollectedAt   string               // RFC 3339, stamped once; evidence entries reuse it
    cache     *payloadCache            // see lazy fetching
}
```

Rules:

- **Steps receive the payload by value.** Anything a step must share, or a
  loader must fill after construction, goes behind a pointer.
- **Record failure to observe separately from the observation.** A nil slice
  means "none found" only if the fetch succeeded; keep an `...Error` field or a
  `...Partial` flag so a step can return `NeedsReview` instead of `Failed`.
- **A target that does not exist is a loader error; a source that cannot be
  read is a field.** A configured path or URL that is missing or malformed is
  a config problem: return an error. A target that exists but whose parts
  cannot be read (a file that will not parse, an endpoint that needs a token)
  sets the matching `...Error` field with the reason.
- **Resolve config in the loader.** `cfg.GetString("owner")` once, into a
  field. Steps that reach into `Config` are hard to test and hide dependencies.
- **Do not embed the loaded struct's secrets.** `--include-payload` writes the
  payload to the results file. Keep tokens and clients unexported, and drop
  the scaffold's `Config *config.Config` field once the loader has resolved
  what steps need: it serializes every config var, a token included.

### Evidence

The SDK harvests evidence from a payload that implements `gemara.HasEvidence`,
which only an *embedded* collector provides. Embed `*gemara.EvidenceCollector`
and set it in the loader:

- A named field (`Evidence *gemara.EvidenceCollector`) promotes nothing, so
  evidence is silently dropped.
- A value embed is lost too: steps receive the payload by value, so each step
  would write into its own copy.
- A nil pointer panics on the first harvest.

go-gemara's own doc example embeds the collector by value and passes a
pointer payload. The SDK passes the payload by value, so follow this page, not
that example. `steps.md`, "Messages are evidence", shows a complete
`gemara.Evidence` entry; recording evidence is optional.

## Loader

```go
// newHTTPClient, fetchRest and RestData are the plugin's own; only the shape matters.
func Loader(cfg *config.Config) (any, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    p := Payload{Owner: cfg.GetString("owner"), Repo: cfg.GetString("repo")}
    p.EvidenceCollector = &gemara.EvidenceCollector{}
    p.APICallCounter = &pluginkit.APICallCounter{}
    client := p.APICallCounter.WrapClient(newHTTPClient(cfg.GetString("token")))

    rest, err := fetchRest(ctx, client, p.Owner, p.Repo)
    if err != nil {
        return nil, fmt.Errorf("github.rest: %w", err)   // typed, source-named
    }
    if rest.DefaultBranch == "" {
        return nil, fmt.Errorf("github.rest: missing default_branch")
    }
    p.RestData = rest

    cfg.Logger.Debug("loaded", "source", "github.rest", "bytes", rest.size)
    return p, nil
}
```

Rules:

- **Bounded.** Every outbound call runs under a context with a timeout. Retry
  only idempotent reads, with a cap; the scanner's `data/retry.go` is the shape.
  Under a hard rate limit (unauthenticated GitHub: 60 an hour) do not retry;
  put the reset time from `X-RateLimit-Reset` in the error.
- **Credentials are optional unless `RequiredVars` says otherwise.** When data
  needs a token the user did not supply, look for an endpoint that exposes it
  without one; failing that, set the `...Error` field to "requires a token" so
  the step returns `NeedsReview`.
- **Validate before returning.** A 200 with an empty required field is a loader
  error. Returning a half-built payload turns loader bugs into false `Failed`s.
- **Fail with a source-named error.** `fmt.Errorf("<source>: <what>: %w", err)`
  so the SDK's loader failure log says which system broke.
- **Log size and shape, not content.** Payload dumps go behind `-l trace`.
- **Independent fetches run concurrently.** A `sync.WaitGroup` (or `errgroup`
  if the plugin already depends on it) over the sources, then assemble; a single slow API should not serialize the run.
- **Count API calls.** Wrap the HTTP client with `pluginkit.APICallCounter` so
  `pvtr benchmark <plugin-binary> --target <name>` can report cost per suite.
- **Declare required vars in `main.go`**, not by checking them in the loader.
  The SDK refuses to run when one is missing, with a clearer message.

## Lazy fetching

Some data is expensive and only a few steps need it (file contents, workflow
YAML, licence lists). Fetch it on first use through a method on the payload,
caching in a pointer-held holder so every by-value copy shares it:

```go
type payloadCache struct {
    workflows        []WorkflowFile
    workflowsFetched bool
}

func (p Payload) Workflows() ([]WorkflowFile, error) {
    if p.cache.workflowsFetched {
        return p.cache.workflows, nil
    }
    ...fetch, then set both fields...
}
```

This is still loader-owned code (package `data`), so the "no I/O in steps"
rule holds: the step calls `payload.Workflows()`, it does not build a client.
Keep the cache unsynchronized only while the SDK runs steps sequentially, and
say so in a comment; a mutex would promise a safety the rest of the plugin
does not have.

## Testing the loader

- Unit-test parsers and validators against fixture bytes.
- For the fetch path, have `Loader` call an inner `load(client, baseURL, ...)`
  so tests can pass an `httptest.Server`'s client and URL; injecting the client
  alone still points at the real API. The scanner's `data/rest_data_mock.go` is the pattern.
- Apply client policy that matters (redirect handling, TLS floor, auth header)
  inside `load()`, not only where the production client is built:
  `srv.Client()` replaces that client, so tests would skip the policy.
- The real network is exercised by `./<bin> debug --target <name>`, not by
  `go test`.
