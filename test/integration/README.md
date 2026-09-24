# integration tests

Boots `services/runtime` and `services/gateway` in-process on loopback over real TLS (HTTPS at the edge, mutual TLS between them) and asserts the golden path with no mocks on the wire. Certificates are minted per run with `packages/utils/certgen`.

```sh
go test ./test/integration/...            # part of task go:test
go test ./test/integration/... -update    # rewrite testdata/ goldens
```
