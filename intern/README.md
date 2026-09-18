# Intern

The trainer. It pulls Echo's mirrored records over HTTP, waits until a full batch of samples is
assembled, and runs one SFT step on the student model that is already loaded in VRAM.

```
your app ──▶ Echo ──▶ your model
               │
               └──▶ records.jsonl ──▶ Intern ──▶ student in VRAM ──▶ checkpoint (disk / S3)
```

Traffic is the dataset. No epochs, no data loader — the more your app is used, the more steps
Intern runs.

Echo records. Intern learns. Numbers belong here. The proxy path never touches Intern.

## Event-driven training

- Intern boots once, loads the student into VRAM, and idles.
- A training step fires when a complete batch of `INTERN_BATCH_SIZE` samples (default `8`) is
  assembled.
- One batch = one step. Then the model idles again and the next batch starts collecting.

A quiet app slows training, it doesn't stop it: a partial batch flushes after
`INTERN_DRAIN_TIMEOUT` and still counts as a step.

## What a sample is

Records arrive as a conversation tree. Intern rebuilds each thread from Echo's chain hashes,
dedups, and produces one training sample per leaf.

Loss is masked to the assistant turns Echo actually witnessed. History is client-supplied and
may have been edited — Intern does not train on it.

## Consume from Echo

```
GET    /v1/records?from=<seq>&follow=true   stream records, stays open when caught up
DELETE /v1/records?upto=<seq>               ack what you've stored
GET    /v1/position                         where Echo is at
```

Ack after you've stored a record, not when you receive it. One consumer only — a second reader
on the same feed starves the first.

## Run it

Bare metal:

```bash
intern --echo http://127.0.0.1:8000 --model meta-llama/Llama-3.1-8B-Instruct
```

Docker:

```yaml
services:
  echo:
    image: vares/echo
    ports: ["8000:8000"]

  intern:
    image: vares/intern
    environment:
      INTERN_ECHO_URL: http://echo:8000
      INTERN_ECHO_TOKEN: secret
      INTERN_MODEL: meta-llama/Llama-3.1-8B-Instruct
      INTERN_BATCH_SIZE: "8"
    volumes: ["./intern-checkpoints:/checkpoints"]
    deploy:
      resources:
        reservations:
          devices: [{ driver: nvidia, count: 1 }]
```

## Configure

| Env | Default | |
|---|---|---|
| `INTERN_ECHO_URL` | *required* | Echo read API base |
| `INTERN_ECHO_TOKEN` | *required* | Bearer token for the read API |
| `INTERN_MODEL` | *required* | Student model to load at boot |
| `INTERN_BATCH_SIZE` | `8` | Samples per training step |
| `INTERN_DRAIN_TIMEOUT` | `10m` | Spill a partial batch after this long |
| `INTERN_LR` | `5e-6` | Learning rate per step |
| `INTERN_CHECKPOINT_DIR` | `/checkpoints` | Keep last-best here |
| `INTERN_UPLOAD_S3` | *unset* | Mirror last-best to this bucket |

## Non-goals

No inference or serving — Intern only trains. No gateway logic, no eval harness in this release.
No multi-consumer fan-out; that is where a queue earns its place, not before.

Building or modifying Intern: see [IMPLEMENTATION.md](IMPLEMENTATION.md).