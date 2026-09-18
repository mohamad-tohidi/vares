# Implementing Intern

Notes for building or modifying Intern. Each item is a real failure mode with one correct path.

## Event-driven loop

Intern is one resident process with the student already in VRAM, running one optimizer step per
completed batch.

- **Never load the model per batch.** Loading is the expensive part; the step is not. Boot once,
  keep the model hot, then wait.
- **Never rebuild the trainer per batch.** Optimizer momentum and LR state are the accumulated
  value of every prior step; rebuilding them resets the student back to batch one.
- The loop is synchronous: wait for a batch, run one step to completion, listen again. No
  background worker racing on the same weights.

## Batching

A batch is `INTERN_BATCH_SIZE` deduped leaf samples. The step fires on either condition:

- a full batch assembles;
- `INTERN_DRAIN_TIMEOUT` elapses with a partial batch — flush it as a shorter step and start over.

A quiet app must slow training, not stop it, and a stalled-batch timer must be alertable so a
dead consumer isn't silently "just slow".

## Consuming Echo

- **Ack after store, never after receive.** Persist the record first, then
  `DELETE /v1/records?upto=<seq>`.
- **Persist the acked `seq`.** On restart, resume from it. Without it a crash replays records
  and double-trains — silent damage to an already-trained student.
- One consumer. Echo deletes on ack, so any second reader on the feed starves the first.
- Keep up: if Intern falls behind Echo's retention, unacked segments are dropped and that
  traffic is gone forever. Lag is measured and surfaced, never silently caught up.

## Reconstruction & sampling

Rebuild the tree from `prefix_hashes`/`chain_hash`. Record `B` extends record `A` when
`A.chain_hash ∈ B.prefix_hashes`.

- Dedup — the same thread appears many times. One sample per leaf, not per record.
- Mask loss to assistant spans Echo actually witnessed. History is client-supplied and may have
  been edited; training on it teaches the student to reproduce text that was never generated.

## The step (TRL)

- One batch → one step. Materialize the step's dataset, run `SFTTrainer` with
  `max_steps=1`, discard the dataset, go back to idle.
- Masked loss is `DataCollatorForCompletionOnlyLM` over the assistant spans — token-level
  masking of the non-training regions, not filtering of the text.
- A batch must fit in VRAM on top of the resident model. OOM mid-step corrupts the loop: catch
  it, shrink the effective batch, and finish the step — never crash into a fresh run.

## Checkpointing

- Evaluate before trusting, and keep **last-best** on disk and optionally S3: the student only
  replaces the previous checkpoint when it holds against the running metric.
- Always leave one usable checkpoint behind, so a crash restarts from last-best — not from boot.

## Secrets

The echo token is Intern's only secret. Send it as a bearer token and never write it into a
checkpoint, a batch, or a log.