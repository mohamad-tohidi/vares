# Intern — v0 Plan

Event-driven SFT trainer. Behaviour contract lives in [README.md](README.md) and
[IMPLEMENTATION.md](IMPLEMENTATION.md); this file is the build plan.

## Target layout

```
intern/
  pyproject.toml                       deps: requests, torch, transformers, trl, datasets, tokenizers, accelerate
  intern/__init__.py
  intern/model.py                      dataclasses: Record, Request, Message, Choice, ToolCall
  intern/config.py                     INTERN_* env -> Config
  intern/log.py                        setup_logging(level)
  intern/consumer.py                   HEARTBEAT, Consumer (stream /v1/records, persist, ack, position)
  intern/tree.py                       Tree.ingest(Record) -> leaf samples; chain_hash threads
  intern/trainer.py                    Trainer.boot(Config); .step(samples) -> StepResult; .evaluate
  intern/checkpoint.py                 CheckpointManager: rolling-eval gate, atomic last-best save
  intern/main.py                       entrypoint (this module under review)
  tests/                               pytest: tree, consumer-position, masking layout, smoke step
```

## Contracts the entrypoint relies on

```python
Config (frozen dataclass):
  echo_url, echo_token, model, batch_size,
  drain_timeout_seconds, retry_delay_seconds,
  lr, checkpoint_dir: Path, log_level

Consumer(cfg)
  .resume()               load persisted seq from checkpoint_dir; start GET /v1/records?from=<seq+1>&follow=true
  .stream() -> Iterator[Record | HEARTBEAT]
                          persist each record FIRST, ack DELETE upto, flush position file;
                          yields HEARTBEAT on read-timeout (idle tick)
  .stop()                 interrupt blocked read (signal handler calls this)
  .close()                final position + ack flush

Tree;  .ingest(record) -> Iterable[Sample]
  rebuilds threads by chain_hash parent resolution (B extends A when A.chain_hash in B.prefix_hashes);
  one Sample per leaf; Sample carries (messages, assistant span, chain_hash).

Trainer.boot(cfg) -> Trainer
  loads last-best checkpoint if present, else INTERN_MODEL; one persistent optimizer,
  cpu only.  .step(samples, partial) -> StepResult(loss, tokens, checkpoint: bool)
  .evaluate(samples) -> float     masked val loss
  .model / .tokenizer

CheckpointManager(cfg, trainer)
  .consider(result, samples, reason)  evaluate on rolling pool of last 8 deduped samples;
                                      replace last-best only on improvement.
```

## Main loop (as implemented)

```
while running:
    feed.next()          # blocking read with idle heartbeat tick
      error -> sleep(retry_delay) + continue
      HEARTBEAT -> if partial-batch open and timeout elapsed: flush(partial=True)
      Record -> tree.ingest -> batch.append
        full batch -> flush(partial=False, reason="full")
    flush() -> trainer.step (one optimizer step, OOM absorbed internally)
              -> checkpoint.consider
    break on SIGINT/SIGTERM -> consumer.close()
```

Key invariant: `trainer.step` is always called with the same trainer instance so optimizer
momentum persists. OOM / GPU memory pressure handled inside Trainer by halving the effective
batch size; if all batches fail the batch is logged and dropped — never the loop.

## Build order & acceptance

1. `intern/model` + `intern/config` + `intern/log` — env parsing, type defs, structured logging.
2. `intern/tree` — chain_hash parent resolution, dedup (by chain_hash + acked cursor), thread
   pruning, leaf sample extraction with correct assistant-span boundaries. **Unit tests.**
3. `intern/consumer` — streaming reader, persistence-before-ack, position file, HEARTBEAT tick
   on idle, graceful stop. **Fake echo server in tests.**
4. `intern/trainer` — boot from last-best or base, step (SFTTrainer reuse with swapped dataset,
   max_steps=1, persistent optimizer), masked labels (all prompt tokens = -100, loss on assistant
   span only — template-agnostic token-boundary mask, not DataCollatorForCompletionOnlyLM),
   eval. **Unit test on mask layout; smoke step on SmolLM2-360M CPU (marked slow).**
5. `intern/checkpoint` — rolling eval pool, last-best gate, atomic tmp-dir→rename save.
6. `intern/main.py` (this entrypoint) — wires 1–5; signal handling; shutdown flush.
7. `demo_traffic.py` (top-level script, outside package) — scripted multi-turn prompts through
   Echo's /v1/chat/completions to generate training data against a hosted provider.

Gate: `uv sync --dev && uv run pytest && uv run python -m intern` (dry-run, needs env).

## Verify (added to AGENTS.md once code lands)

```
cd intern && uv sync --dev && uv run pytest
# e2e (needs INTERN_ECHO_URL, INTERN_ECHO_TOKEN, INTERN_MODEL set):
uv run python -m intern
```

## Out of scope for v0

S3 upload (documented no-op), MPS/GPU acceleration, Docker, eval harness, tool_call
tool-result follow-ups (assistant tool_calls rendered as text in the sample — a known
limitation, noted in README). No epochs, no data loader, no data files.

# Fail-mode notes carried from IMPLEMENTATION.md

- **Never load the model per batch; never rebuild the trainer per batch.**
- Ack after store, never after receive. Persist acked seq so crash restarts don't
  double-train. One consumer by design in v0.
- Keep up: if Intern falls behind Echo's retention, unacked segments are dropped and
  that traffic is gone forever. Lag is logged, never silently swallowed.
- Leave one usable checkpoint behind at all times — a crash restarts from last-best,
  not from boot.