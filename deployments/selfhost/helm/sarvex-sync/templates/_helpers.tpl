{{- define "sarvex-sync.name" -}}
sarvex-sync
{{- end -}}

{{- define "sarvex-sync.labels" -}}
app.kubernetes.io/name: {{ include "sarvex-sync.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
sarvex.example.com/plane: sync
{{- end -}}
