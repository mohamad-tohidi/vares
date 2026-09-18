# Echo

A transparent proxy for OpenAI-compatible chat completions.
It forwards every request untouched, and writes the input + output to a log on the side.

```
your app ──▶ Echo ──▶ your model
              │
              └──▶ records.jsonl ──▶ Apprentice pulls over HTTP
```

Echo records. It does not interpret. Conversation reconstruction, dedup and sampling all
live in `Apprentice`.

## Run it

**Docker** — stop publishing the model's port, publish Echo's instead. Clients see no change.

```yaml
services:
  vllm:
    image: vllm/vllm-openai
    # no `ports:` — reachable only inside the network now

  echo:
    image: vares/echo
    ports: ["8000:8000"]
    environment:
      ECHO_UPSTREAM: http://vllm:8000
    volumes: ["./echo-data:/data"]
```

**Bare metal** — move the model, let Echo take the port your app already uses.

```bash
vllm serve <model> --host 127.0.0.1 --port 8001
echo --listen :8000 --upstream http://127.0.0.1:8001
```

**Hosted provider** — point your app at Echo instead of the provider.

```bash
echo --listen :8000 --upstream https://api.openai.com
```

Your app keeps sending its own key. Echo forwards it and never logs it.

## Read the data

```
GET    /v1/records?from=<seq>&follow=true   stream NDJSON, stays open when caught up
DELETE /v1/records?upto=<seq>               release what you've stored
GET    /v1/position                         first_seq / last_seq on disk
```

Ack after you've stored a record, not when you receive it. One consumer only.

## A record

```json
{
  "seq": 1483,
  "ts": "2026-09-18T12:47:02.113Z",
  "model": "meta-llama/Llama-3.1-8B-Instruct",
  "stream": true,
  "request":  { "messages": [ ... ] },
  "response": { "choices": [ ... ] },
  "prefix_hashes": ["a91c…", "4fd2…"],
  "chain_hash": "e22a…"
}
```

Bodies are as they appeared on the wire; streamed responses are reassembled into the
non-streaming shape. The hashes let `Apprentice` link a request to the one it extends.

## Configure

| Env | Default | |
|---|---|---|
| `ECHO_UPSTREAM` | *required* | Model API base URL |
| `ECHO_LISTEN` | `:8000` | Listen address |
| `ECHO_TOKEN` | *required* | Bearer token for the read API |
| `ECHO_DATA` | `/data` | Log directory |
| `ECHO_RETENTION` | `72h` | Drop acked segments older than this |
| `ECHO_MAX_BODY` | `8MB` | Skip capture above this size, still proxy |

## Non-goals

No retries, caching, rate limiting, routing or key management. Echo is not a gateway.

Building or modifying Echo: see [IMPLEMENTATION.md](IMPLEMENTATION.md).
