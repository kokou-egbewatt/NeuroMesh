# config

- `config.Load(path, &dst)` reads a YAML file into `dst`, rejects unknown keys, and reports whether the file existed. A missing file is not an error, but every service logs a warning naming the path.
- `config.StringEnv(key, fallback)` reads an environment variable with a default. Services apply env vars (`RUNTIME_ADDR`, `GATEWAY_ADDR`, `CONFIG_PATH`) on top of the file.
- `config.TLS` is the transport section every service shares (`cert_file`, `key_file`, `ca_file`, `insecure`, `allowed_client_names`). `ServerConfig(requireClientCert)` and `ClientConfig(serverName)` build TLS 1.3 configs from it; the file names match a mounted cert-manager Secret (`tls.crt`, `tls.key`, `ca.crt`).
