# openchain_tracer

An opensource tracer backend for [openchain](https://github.com/openchainxyz/openchain-monorepo), based on [erigon](https://github.com/ledgerwatch/erigon).

Contract labels, function names are not supported yet.

Some gas calculation might be incorrect. The last `sstore`/`sload` before revert might be incorrect.

## Usage

1. Put `openchain.go` into `<erigon_source_path>/eth/tracers/native/`, and then recompile. Currently tested versionL `v2.48.1`.
2. Start `proxy.py`. It requires `flask` to run.
3. In [openchain-monorepo](https://github.com/openchainxyz/openchain-monorepo), set `NEXT_PUBLIC_API_HOST=http://127.0.0.1:2000`.