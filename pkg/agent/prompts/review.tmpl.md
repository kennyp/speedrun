Analyze this GitHub pull request:

PR: #{{ .Number }} - {{ .Title }}

**Changes:**
- Files changed: {{ .ChangedFiles }}
- Lines added: {{ .Additions }}
- Lines deleted: {{ .Deletions }}
- Total changes: {{ sum .Additions .Deletions }}

**CI Status:** {{ .CIStatus }}

{{ if .Reviews }}
**Existing Reviews:**
{{ range .Reviews }}
- {{ .User }}: {{ .State }}
{{ end }}
{{ else }}
**Existing Reviews:** None
{{ end }}
