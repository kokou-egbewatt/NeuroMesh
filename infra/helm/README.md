# helm

One chart per service (`<service>/`), plus umbrella charts wrapping the observability stack. Every chart carries probes wired to the service's health check, a `VMServiceScrape`, resource requests from measurement, and is linted and templated in CI. They deploy to the local k3s cluster only for now. Remote environments come much later (#90), and will reuse these charts unchanged.
