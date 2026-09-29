# acme-api

A small HTTP API: session-authenticated endpoints in front of a read-through
cache, configured per storage provider.

## Install

```sh
go instal ./cmd/acme-api
```

## Test

```sh
make test
```

The test target runs with the race detector on.
