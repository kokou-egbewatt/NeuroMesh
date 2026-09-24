# scripts

Developer scripts that are not CI gates. CI gate scripts live in [`ci/scripts`](../ci/scripts).

- `install-tools.sh`: the one place task, buf, the protoc plugins, golangci-lint, air and lefthook are pinned. `task tools:install` runs it; CI installs through it too.
- `doctor.sh` (`task doctor`), `test-in-docker.sh` (`task go:test:docker`), `windows-trust-ca.ps1` (`task certs:trust`), `release-next.sh` (`task release:next`), `new-doc.sh` (`task adr:new`, `task rfc:new`).
