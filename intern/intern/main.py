from __future__ import annotations

import signal
import time
from collections.abc import Iterator
from typing import NoReturn

from intern.checkpoint import CheckpointManager
from intern.config import Config, load_config
from intern.consumer import HEARTBEAT, Consumer
from intern.log import setup_logging
from intern.model import Record
from intern.trainer import StepResult, Trainer
from intern.tree import Sample, Tree


def main() -> NoReturn:
    cfg: Config = load_config()
    log = setup_logging(cfg.log_level)
    log.info("intern boot", batch_size=cfg.batch_size, model=cfg.model, checkpoints=cfg.checkpoint_dir)

    trainer = Trainer.boot(cfg)
    checkpoint = CheckpointManager(cfg, trainer)
    consumer = Consumer(cfg)
    consumer.resume()
    tree = Tree()

    running = True

    def stop(_signum: int, _frame: object) -> None:
        nonlocal running
        running = False
        consumer.stop()

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)

    batch: list[Sample] = []
    opened: float | None = None
    steps = 0

    def flush(partial: bool, reason: str) -> None:
        nonlocal batch, opened, steps
        if not batch:
            return
        steps += 1
        result: StepResult = trainer.step(batch, partial=partial)
        checkpoint.consider(result, samples=batch, reason=reason)
        log.info(
            "step done",
            step=steps,
            samples=len(batch),
            loss=result.loss,
            tokens=result.tokens,
            checkpointed=result.checkpoint,
        )
        batch = []
        opened = None

    feed: Iterator[Record | object] = consumer.stream()
    while running:
        try:
            item = next(feed)
        except StopIteration:
            break
        except Exception as exc:
            log.warning("feed error, waiting", err=str(exc))
            time.sleep(cfg.retry_delay_seconds)
            continue

        if item is HEARTBEAT:
            if opened is not None and time.monotonic() - opened >= cfg.drain_timeout_seconds:
                wait = int(time.monotonic() - opened)
                log.info("draining partial batch", waited=wait, samples=len(batch))
                flush(partial=True, reason="drain")
            continue

        for sample in tree.ingest(item):
            batch.append(sample)
            if opened is None:
                opened = time.monotonic()
            if len(batch) >= cfg.batch_size:
                flush(partial=False, reason="full")

    consumer.close()
    log.info("intern stopped", steps=steps)