Integration tests were moved into the Go module at **`CELLS/source/tests/`** so `go test ./...` from `CELLS/source` includes them.

```bash
cd CELLS/source
export CELLS_METRICS_REGISTER_STRICT=1
CGO_ENABLED=0 go test ./... ./tests/... -short
```
