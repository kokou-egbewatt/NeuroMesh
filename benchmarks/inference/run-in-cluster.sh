#!/bin/sh
# Entrypoint of the bench image (Dockerfile here): two recorded runs against TARGET and the
# repeatability check, the cluster counterpart of `task bench:repeat`. Each run's JSON goes to the
# log between markers, so scripts/services.sh can keep it in benchmarks/inference/results/.
#   TARGET  the gateway URL (https://gateway:8443 inside the cluster)
#   CA      the CA the gateway's certificate chains to
#   COMMIT  the commit to record
set -eu
for run in 1 2; do
  bench -target "$TARGET" -ca "$CA" -commit "$COMMIT" -out "/tmp/cluster-run$run.json" "$@"
  echo "--- json run$run"
  cat "/tmp/cluster-run$run.json"
  echo "--- end"
done
bench -compare /tmp/cluster-run1.json,/tmp/cluster-run2.json
