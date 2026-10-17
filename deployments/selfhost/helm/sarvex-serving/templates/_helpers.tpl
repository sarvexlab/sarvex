{{- define "sarvex-serving.name" -}}
sarvex-serving
{{- end -}}

{{- define "sarvex-serving.labels" -}}
app.kubernetes.io/name: {{ include "sarvex-serving.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
sarvex.example.com/plane: serving
{{- end -}}
