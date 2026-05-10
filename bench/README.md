# Benchmarks

This directory contains Harbor integration for running librecode against external benchmark tasks.

## Harbor adapter

`bench/harbor/librecode_agent.py` is a Harbor installed-agent adapter. It uploads a locally built `librecode` binary into each Harbor task container and runs librecode non-interactively with extensions disabled.

Harbor agent docs: <https://www.harborframework.com/docs/agents>

## Run

Build librecode first:

```bash
mise exec -- task build
```

Then run Harbor from the repository root:

```bash
PYTHONPATH=$PWD harbor run \
  -d terminal-bench/terminal-bench-2-1 \
  --agent-import-path bench.harbor.librecode_agent:LibrecodeAgent \
  -k 5 \
  -n 8
```

Flags:

- `--agent-import-path bench.harbor.librecode_agent:LibrecodeAgent` tells Harbor to use this adapter.
- `-d terminal-bench/terminal-bench-2-1` selects the Harbor dataset/task collection.
- `-k 5` runs five attempts per trial.
- `-n 8` runs eight trials concurrently.

If your binary is somewhere else:

```bash
PYTHONPATH=$PWD LIBRECODE_BINARY=/path/to/librecode harbor run \
  -d terminal-bench/terminal-bench-2-1 \
  --agent-import-path bench.harbor.librecode_agent:LibrecodeAgent \
  -k 5 \
  -n 8
```

If you prefer installing inside the container instead of uploading a binary:

```bash
PYTHONPATH=$PWD \
LIBRECODE_INSTALL_COMMAND='your install command here' \
harbor run \
  -d terminal-bench/terminal-bench-2-1 \
  --agent-import-path bench.harbor.librecode_agent:LibrecodeAgent \
  -k 5 \
  -n 8
```

## Docker Hub rate limits

Harbor tasks may pull many Docker images. If pulls fail with an unauthenticated rate-limit error, run:

```bash
docker login
```

Then rerun the Harbor command. Lower `-n` if pulls or local resources are the bottleneck.

## Go render microbenchmarks

For local terminal render microbenchmarks, use Go benchmarks directly:

```bash
mise exec -- go test -bench=. -benchmem ./internal/terminal
```
