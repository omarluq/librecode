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
task bench
```

`task bench` builds librecode, copies `${LIBRECODE_BENCH_AUTH_DIR:-${LIBRECODE_HOME:-$HOME/.librecode}}` to a temporary writable mount, and passes that mount to Harbor as `LIBRECODE_HOME`.

Useful overrides:

```bash
LIBRECODE_BENCH_N=2 task bench
LIBRECODE_BENCH_K=1 LIBRECODE_BENCH_N=1 task bench
LIBRECODE_BENCH_DATASET=terminal-bench/polyglot-rust-c task bench
LIBRECODE_BENCH_AUTH_DIR=$HOME/.librecode task bench
```

Flags/env:

- `LIBRECODE_BENCH_AGENT_IMPORT_PATH` defaults to `bench.harbor.librecode_agent:LibrecodeAgent`.
- `LIBRECODE_BENCH_DATASET` defaults to `terminal-bench/terminal-bench-2-1`.
- `LIBRECODE_BENCH_K` defaults to `5` attempts per trial.
- `LIBRECODE_BENCH_N` defaults to `4` concurrent trials.

If your binary is somewhere else:

```bash
LIBRECODE_BINARY=/path/to/librecode task bench
```

If you prefer installing inside the container instead of uploading a binary:

```bash
LIBRECODE_INSTALL_COMMAND='your install command here' task bench
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
