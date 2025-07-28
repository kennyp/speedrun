Analyze this GitHub pull request:

PR: #{{ .Number }} - {{ .Title }}
URL: {{ .PRURL }}

**Changes:**
- Files changed: {{ .ChangedFiles }}
- Lines added: {{ .Additions }}
- Lines deleted: {{ .Deletions }}
- Total changes: {{ sum .Additions .Deletions }}
{{ if .HasConflicts }}
- **⚠️ Has merge conflicts**
{{ end }}

{{ if .CheckDetails }}
**CI Checks:**
{{ range .CheckDetails }}
- {{ .Name }}: {{ .Status }}{{ if .Description }} - {{ .Description }}{{ end }}
{{ end }}
{{ else if .CIStatus }}
**CI Status:** {{ .CIStatus }}
{{ else }}
**CI Status:** No checks found
{{ end }}

{{ if .Reviews }}
**Existing Reviews:**
{{ range .Reviews }}
- {{ .User }}: {{ .State }}
{{ end }}
{{ else }}
**Existing Reviews:** None
{{ end }}
