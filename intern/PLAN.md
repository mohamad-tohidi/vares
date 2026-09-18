# Intern — v0 Plan (Simplified)

Event-driven SFT trainer. Behaviour contract lives in [README.md](README.md) and
[IMPLEMENTATION.md](IMPLEMENTATION.md); this file is the build plan. Simplified from the original 8‑module design.

## Target layout

```
intern/
  pyproject.toml                       deps: requests, torch, transformers, trl, datasets, tokenizers, accelerate
  intern/__init__.py
  intern/main.py                       entrypoint (this is the module under review)
  intern/config.py                     INTERN_* env → Config
  intern/consumer.py                   HEARTBEAT, Consumer (stream, ack after consume, position, heartbeat)
  intern/trainer.py                    Trainer (boot, step, eval, last‑best save — checkpoint folded in)
  tests/                               pytest: dedup, masking layout, smoke step (marked)
```

All code lives in **four modules** only. No separate `model.py`, `store.py`,
`tree.py`, `checkpoint.py`, `log.py` — dicts replace dataclasses, ack‑after‑consume
replaces raw persistence, eval/save live in `trainer`, and standard `logging`
is configured in `main`.

## Contracts the entrypoint relies on

```python
Config (frozen dataclass):
  echo_url, echo_token, model, batch_size,
  drain_timeout_seconds, retry_delay_seconds,
  lr, checkpoint_dir: Path, log_level

Consumer(cfg)
  .resume()                load persisted seq from checkpoint_dir; start GET /v1/records?from=<seq+1>&follow=true
  .stream() -> Iterator[dict | HEARTBEAT]
                          persist each record FIRST (append to local raw file),
                          ack DELETE upto, flush position file;
                          yields HEARTBEAT on read‑timeout (idle tick)
  .stop()                  interrupt blocked read (signal handler calls this)
  .close()                 final position + ack flush

Trainer.boot(cfg) -> Trainer
  loads last‑best checkpoint if present, else INTERN_MODEL; one persistent optimizer,
  cpu only.  .step(samples) -> StepResult(loss, tokens)
  .evaluate(samples) -> float     masked val loss on a rolling pool (last 8 deduped samples)
  .save_last()                  atomic last‑best save (tmp‑dir → rename)

main loop (as implemented):

while running:
    next(feed)          # blocking read with idle heartbeat tick
      error -> sleep(retry_delay) + continue
      HEARTBEAT -> if partial‑batch open and timeout elapsed: flush(partial=True)
      dict -> tree-less sampler: dedup via recent‑hash deque; append to batch
        full batch -> trainer.step (one optimizer step, OOM absorbed internally)
                -> trainer.consider(result)
    break on SIGINT/SIGTERM -> consumer.close()
```

## Build order & acceptance

1. `intern/config` + `intern/log` — env parsing, structured logging.
2. `intern/consumer` — streaming reader, ack after consume, position file,
   HEARTBEAT tick, dedup deque (recent hashes). **Unit tests with fake echo server.**
3. `intern/trainer` — boot from last‑best or base, step (SFTTrainer reuse with
   swapped dataset, max_steps=1, persistent optimizer), masked labels (all prompt
   tokens = -100, loss on assistant span only — template‑agnostic token‑boundary
   mask), eval. **Unit test on mask layout; smoke step on SmolLM2-360M CPU (marked slow).**
4. `intern/main.py` (this entrypoint) — wires 1–3; signal handling; drain timer.
5. e2e: echo + hosted provider + demo_traffic.py + one intern step (batch 2).

Gate: `uv sync --dev && uv run pytest && uv run python -m intern` (dry‑run,
needs env).

## Verify (added to AGENTS.md once code lands)

```
cd intern && uv sync --dev && uv run pytest
# e2e (needs INTERN_ECHO_URL, INTERN_ECHO_TOKEN, INTERN_MODEL set):
uv run python -m intern
```

## Out of scope for v0

S3 upload, MPS/GPU acceleration, Docker, eval harness/evals, tool_call
tool‑result follow‑ups (assistant tool_calls rendered as text in the sample),
epochs/data loader, raw record journal (ack‑after‑consume is enough).

## Key simplification vs original plan

- **No tree / no chain‑hash parent resolution:** Each record is a standalone sample.
  Dedup uses a bounded deque of recent record hashes (kept in `main.py`);
  crash‑replay is prevented by the acked position file only. For normal
  full‑history clients (the OpenAI‑compatible norm) one sample per record ≈
  one sample per leaf; edited/truncated history may produce near‑duplicate
  samples but loss is masked to witnessed assistant spans only, so harmless.
- **Ack after consume, not after store:** Intern acks `DELETE upto=<seq>` immediately
  after a record is handed to the batch buffer. Since v0 drops failed batches by
  design, persisting every record to disk adds no safety net and is omitted.
  Crash‑safety rests on the acked seq + position file, which is already required
  for resume.
- **Checkpoint folded into Trainer:** `CheckpointManager` is eliminated; `Trainer`
  owns the rolling‑eval pool, last‑best gate, and atomic save. One fewer module
  to import and maintain.
- **Record type is a plain dict** (no `model.py` dataclass). The consumer yields
  dicts; the trainer builds the masked-label tensor from the dict fields.
  One fewer module, fewer import cycles.
- **Standard library logging** replaces the separate `log.py` module; `config.py`
  is a minimal dataclass living alongside `main.py`.