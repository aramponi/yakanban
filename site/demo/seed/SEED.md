# Demo seed

The starting state of `aramponi/acme-api`, the throwaway repository the hero
clip is recorded on. Each take resets that repository to this tree, so every
recording starts from the same four problems:

| Planted problem | How it shows |
|---|---|
| Typo in the README install command (`go instal`) | reading the README |
| An expired session answers 500 instead of 401 | `TestExpiredSessionIs401` fails |
| `Cache.Invalidate` walks the map without the lock | `make test` reports a data race |
| `config.Load` handles every provider in one function | the TODO above it |

It is its own Go module, so the yakanban build and `go test ./...` never see
it. Do not fix the bugs here: they are the script.
