# scripts

Developer scripts that are not CI gates. CI gate scripts live in [`ci/scripts`](../ci/scripts).

- `install-tools.sh`: the one place task, buf, the protoc plugins and golangci-lint are pinned. `task tools:install` runs it; CI installs through it too.
