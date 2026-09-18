# Echo — v0 Plan (Simplified)

Transparent OpenAI-compatible capture proxy. Behaviour contract lives in
[README.md](README.md) and [IMPLEMENTATION.md](IMPLEMENTATION.md); this file
is the build plan. Simplified from the original 8‑package design.

## Target layout

```
echo/
  go.mod                                module vares/echo, go 1.26
  cmd/echo/main.go                      entrypoint (this is the module under review)
  internal/proxy/proxy.go               catch-all ReverseProxy + capture hook
  internal/store/store.go               NDJSON segments, writer goroutine, fsync,
                                          ack cursor, retention, follow‑read
  internal/api/api.go                   read API (records/position/delete) + bearer auth
  *_test.go                             per package
```

All code lives in **package main** or in the three internal packages listed above.
No separate `model` or `hashes` package — the only wire type is `Record` defined
in `store`, and the only hash is a single `sha256` hex digest over canonical
non‑system messages.

## Contracts the entrypoint relies on

```go
config.Load() (*config.Config, error)
  Config{ UpstreamURL *url.URL, Listen, Token, DataDir string,
          Retention, FlushEvery time.Duration, SegmentBytes, MaxBody int64,
          RecordHash func(r Record) string }   // single hash func

store.Open(dir string, opts store.Options{ SegmentBytes, Retention int64/…, FlushEvery, Logger })
  (*store.Store, error)
  *Store implements capture.Recorder:  Track(Record) bool   // bounded channel; false => dropped+counter
  Position() (first, last uint64)
  Ack(upto uint64) error
  Stream(ctx, from uint64, follow bool) (<-chan Record, error)
  Close() error

proxy.New(proxy.Options{ Upstream *url.URL, MaxBody int64, Recorder capture.Recorder, Logger })
  (*proxy.Proxy, error)   // http.Handler
  POST ***/chat/completions (suffix match) => captureAndProxy; everything else => verbatim passthrough
  The proxy computes a single Record.Hash = SHA‑256( canonical(non‑system messages) )
  and passes the Record to the Store.

api.New(api.Options{ Store *store.Store, Token string, Logger }) (*api.API, error)
api.Match(r *http.Request) bool            // exact paths /v1/records and /v1/position
  *API implements http.Handler (requires Bearer == Token on all three endpoints)
```

## Build order & acceptance

1. `internal/store` — Record struct + segments, ack cursor, retention, follow‑read.
   Unit tests.
2. `internal/proxy` — ReverseProxy FlushInterval=-1, capture hook, single‑hash
   over non‑system messages. httptest e2e.
3. `internal/api` — /v1/records, /v1/position, DELETE ack, 401 on bad token.
   Unit tests.
4. `cmd/echo/main.go` (this entrypoint) wires 1–3, graceful shutdown.
5. Full e2e: scripted upstream → proxy → store → api → delete‑ack → retention.

Gate: `go vet ./... && go test ./... && go build ./cmd/echo`

## Verify (added to AGENTS.md once code lands)

```
cd echo && go test ./... && go build ./cmd/echo
```

## Out of scope for v0

Docker image, metrics endpoint, NATS/JetStream, multiple consumers, gzip of logs,
chain‑hash / prefix‑hash tree, caching/retry/routing.

## Key simplification vs original plan

- **One hash instead of a chain:** Echo computes `sha256( canonical(non‑system messages) )`
  and stores it as `Record.Hash`. Intern dedup uses this hash in a bounded deque; no
  parent‑resolution tree is needed. The messages themselves already carry the full
  conversation history in the OpenAI‑compatible API, so “one sample per leaf” ≈
  “one sample per record” for the common case of full‑history clients.
- **Dropped `prefix_hashes` / `chain_hash` / tree rebuild:** No `internal/hashes`,
  no `internal/capture` tree logic, no parent‑resolution. The proxy still reassembles
  SSE when `stream=true` (mark `truncated=true` on client disconnect), but the
  resulting record identity is just the single hash.