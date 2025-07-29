Analyze this GitHub pull request:

PR: #{{ .Number }} - {{ .Title }}
URL: {{ .PRURL }}
{{ if .Author }}
**Author:** {{ .Author }}
{{ end }}
{{ if .Labels }}
**Labels:** {{ range .Labels }}{{ . }} {{ end }}
{{ end }}
{{ if .RequestedReviewers }}
**Review Requested From:** {{ range .RequestedReviewers }}{{ . }} {{ end }}
{{ end }}

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

{{ if .Description }}
**PR Description Preview:**
{{ .Description }}
{{ end }}

**Analysis Notes:**
- Check file extensions to determine PR type (*.md = documentation)
- For documentation PRs, assess clarity and technical accuracy
- For code owner reviews, focus on your owned components
- For personal review requests, check PR description for specific asks
