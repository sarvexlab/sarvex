{{- define "sarvex-control.name" -}}
sarvex-control
{{- end -}}

{{- define "sarvex-control.labels" -}}
app.kubernetes.io/name: {{ include "sarvex-control.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
sarvex.example.com/plane: control
{{- end -}}
