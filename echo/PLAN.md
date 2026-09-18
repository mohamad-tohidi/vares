# Echo — v0 Plan

Transparent OpenAI-compatible capture proxy. Behaviour contract lives in
[README.md](README.md) and [IMPLEMENTATION.md](IMPLEMENTATION.md); this file is the build plan.

## Target layout

```
echo/
  go.mod                                module vares/echo, go 1.26
  cmd/echo/main.go                      entrypoint (this is the module under review)
  internal/config/config.go             ECHO_* env -> Config
  internal/model/model.go               wire types: Record, Request, Response, Message, ToolCall
  internal/hashes/hashes.go             message canonicalization + chain hashes
  internal/capture/capture.go           request extraction + SSE reassembly into Response
  internal/store/store.go               NDJSON segments, writer goroutine, fsync, acks, retention, follow-read
  internal/proxy/proxy.go               catch-all ReverseProxy handler + capture hook
  internal/api/api.go                   read API (records/position/delete) + bearer auth
  *_test.go                             per package
```

## Contracts the entrypoint relies on

```go
config.Load() (*config.Config, error)
  Config{ UpstreamURL *url.URL, Listen, Token, DataDir string,
          Retention, FlushEvery time.Duration, SegmentBytes, MaxBody int64 }

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

api.New(api.Options{ Store *store.Store, Token string, Logger }) (*api.API, error)
api.Match(r *http.Request) bool            // exact paths /v1/records and /v1/position
  *API implements http.Handler (requires Bearer == Token on all three endpoints)
```

`main.go` wires them in this order: config → store → proxy → api → root handler (exact API
paths first, proxy for everything else) → server with no `WriteTimeout` → graceful drain.

## Build order & acceptance

1. `internal/model` — frozen wire types used by capture/store/api; the only shared vocabulary.
2. `internal/config` — env parsing + defaults (`ECHO_LISTEN=:8000`, `ECHO_RETENTION=72h`,
   `ECHO_MAX_BODY=8MB`; `ECHO_UPSTREAM`/`ECHO_TOKEN` required). Unit tests.
3. `internal/hashes` — canonical each message (stable key order), chain over **non-system**
   messages: `h0=H(canon m0)`, `hi=H(h[i-1] || canon m[i])`; `prefix_hashes` = strict prefixes,
   `chain_hash` = final (full SHA-256 hex). Unit tests.
4. `internal/capture` — parse request body (model, messages, stream, stream_options); reassemble
   SSE: delta.content concat, tool_calls concat by index, `n>1` parallel choices, trailing
   `usage`, `truncated=true` on client disconnect; fail open on invalid JSON / oversized body /
   duplicate `stream_options`. Unit tests over fixtures.
5. `internal/store` — append NDJSON segments, size rotation, batch fsync ~50ms, persisted seq
   counter + ack cursor, retention deletes whole acked segments (loud log if an unacked one is
   dropped), `follow=true` tails appends. Unit tests incl. restart-from-ack.
6. `internal/proxy` — ReverseProxy `FlushInterval=-1`, `ResponseHeaderTimeout`, high
   `MaxIdleConnsPerHost`, no server write timeout, strip `Accept-Encoding` upstream, host-header
   logic (client host for local upstreams, target host for https providers), strip
   Authorization/api-key/Proxy-Authorization from records. httptest e2e.
7. `internal/api` — NDJSON records stream from `from`, stay-open when `follow`, position,
   delete ack, 401 on bad token. Tests with httptest client.
8. `cmd/echo/main.go` (this entrypoint) — wires 1–7, graceful shutdown drains writer.
9. Full loop e2e: scripted upstream → proxy → store → api → delete-ack → retention.

Gate: `go vet ./... && go test ./... && go build ./cmd/echo`

## Verify (added to AGENTS.md once code lands)

```
cd echo && go test ./... && go build ./cmd/echo
```

## Out of scope for v0

Docker image, metrics endpoint, NATS/JetStream, multiple consumers, retry/cache/routing,
gzip of logs. Echo records; it does not interpret.

# Fail-mode notes carried from IMPLEMENTATION.md

- Passthrough is default, capture is exception: silent non-capture is the worst failure mode.
- Nothing in the capture path may fail the proxy path (bounded channel, fail-open).
- No server `WriteTimeout` — SSE streams are long-lived.
- One consumer deletes-on-ack; a second reader starves the first by design in v0.