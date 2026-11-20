{{/*
The environment both containers share: the model, and every path the engine writes moved onto the
PVC, since the root filesystem is read-only and the image's defaults are under root's home.
*/}}
{{- define "vllm.env" -}}
- { name: MODEL, value: {{ .Values.model | quote }} }
- { name: HOME, value: /data/cache }
# The image has no passwd entry for this uid, and torch derives its cache path through
# getpass.getuser(), which reads these before failing on the passwd lookup.
- { name: USER, value: vllm }
- { name: LOGNAME, value: vllm }
- { name: HF_HOME, value: /data/cache/huggingface }
- { name: XDG_CACHE_HOME, value: /data/cache }
- { name: VLLM_CACHE_ROOT, value: /data/cache/vllm }
- { name: TRITON_CACHE_DIR, value: /data/cache/triton }
- { name: VLLM_NO_USAGE_STATS, value: "1" }
- { name: DO_NOT_TRACK, value: "1" }
{{- end }}
