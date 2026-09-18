# AGENTS.md

Working rules for this repo.

## What this is

Vares = an online LLM distillator.

- `echo/` — captures your app's model traffic
- `intern/` — reads the captures, batches them, trains the student

## The approach

1. Build something simple that works.
2. Keep it modular and clean.
3. Add features one small step at a time.

When in doubt: ship the smallest change that works today, not the design that works someday.

## Conventions

- Each module ships two docs: `README.md` (usage) and `IMPLEMENTATION.md` (design notes, failure modes).
- Read a module's docs before editing it.
- Keep env naming consistent: `ECHO_*` and `INTERN_*`.

## Verify

- Build/test echo: `cd echo && go test ./... && go build ./cmd/echo`
- Build/test intern: `cd intern && uv sync --dev && uv run pytest`
- Run intern: `cd intern && uv run python -m intern` (needs `INTERN_ECHO_URL`, `INTERN_ECHO_TOKEN`, `INTERN_MODEL`)


## echo
its stack is golang. it remains as a simple transparent proxy, that echo's the data to intern.


## intern

the trainer, that pulls data from echo. written in python and hugginface libraries, like datasets, TRL, transformers.