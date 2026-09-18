# Implementing Echo

Notes for building or modifying Echo. Each item is a real failure mode with one correct path.

## Routing

Passthrough is the default; capture is the exception. One catch-all handler, one special case:

```go
if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions") {
    captureAndProxy(w, r, proxy)
    return
}
proxy.ServeHTTP(w, r)
```

Everything else — `/v1/models`, `/health`, `/metrics`, routes added next release — forwards
verbatim, so Echo never needs to know the upstream's full API.

**Match on suffix, not exact path.** It survives `/openai/v1/…`, vLLM's `--root-path`, and
Azure's `/openai/deployments/{name}/chat/completions`. Silent non-capture is the worst
failure mode Echo has.

**Don't use `http.ServeMux`.** It cleans paths and will 301-redirect `/v1//chat/completions`.
A plain `http.HandlerFunc` switching on `r.URL.Path` mutates nothing.

## Streaming

Set `FlushInterval: -1` on the `ReverseProxy`. Without it, streamed tokens are buffered and
Echo adds latency to every call.

Forward each chunk byte-for-byte while separately accumulating deltas into a final message:

- `tool_calls` arrive as string fragments keyed by `index`, concatenate them
- `n > 1` means several `choices` accumulating in parallel
- `usage` arrives in a trailing chunk only when `stream_options.include_usage` is set
- a client disconnect mid-stream yields a partial record — mark it `"truncated": true`

## Never degrade the proxy path

- Hand the finished record to a bounded channel drained by one writer goroutine. Channel
  full → drop and bump a counter. A slow disk must not add latency to inference.
- Fail open on everything: invalid JSON, body over `ECHO_MAX_BODY`, full disk. Nothing
  originating in the capture path may fail the proxy path.
- Strip `Accept-Encoding` going upstream, or you log gzipped bytes.

## Transport

- No server `WriteTimeout` — SSE streams are long-lived
- Use `Transport.ResponseHeaderTimeout`, not an overall deadline
- Raise `MaxIdleConnsPerHost`; the default of 2 serializes concurrent calls and looks like upstream lag
- `ReverseProxy` already strips hop-by-hop headers and proxies `Upgrade` requests
- `r.Out.Host`: prefer the client's `Host` for self-hosted upstreams, the target's for HTTPS providers (SNI/routing)

## Storage

Append NDJSON, batch `fsync` every ~50 ms or N records, rotate segments by size.

Delete **whole segments** once their highest `seq` is acked — never rewrite a file to remove
lines. Log loudly when retention drops a segment that was never acked; that is silent data
loss otherwise.

Delete-on-ack implies exactly one consumer. A second consumer (eval set, inspection UI) is
the point where NATS JetStream earns its keep — not before.

## Secrets

Drop `Authorization`, `api-key` and `Proxy-Authorization` before writing a record. They are
still forwarded upstream. Never log them, not even redacted by prefix.

## Conversation hashes

Later requests usually contain earlier ones as a prefix. Echo does not dedup; it only emits
hashes so `Intern` can rebuild the tree cheaply.

```
h₀ = H(canon(msg₀))
hᵢ = H(hᵢ₋₁ ‖ canon(msgᵢ))
```

Record `B` extends record `A` when `A.chain_hash ∈ B.prefix_hashes`.

Exclude volatile system-prompt content (timestamps, injected RAG context) from the chain, or
every conversation fragments into orphan roots.

Downstream, `Intern` trains one sample per leaf with loss masked to assistant spans, and
puts loss only on assistant messages Echo actually witnessed — history is client-supplied
and may have been edited.
