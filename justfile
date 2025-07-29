# Just Settings

set unstable := true
set script-interpreter := ['bash', '-eu', '-o', 'pipefail']

# Global Vars

banner_prefix := "\n"
step_prefix := ""

# Paths
bins := join(justfile_directory(), "bin")
cmds := join(justfile_directory(), "cmd")


alias h := help
alias b := build
alias g := generate
alias t := test

# Helper Commands

golangci-lint := "go tool golangci-lint"
staticcheck := "go tool staticcheck"
modernize := "go tool golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize"

# Show this
@help: (banner "Recipes") && (banner "")
    just --list --list-heading "" --justfile {{ justfile() }}

# compile speedrun binary
@build: (_build "speedrun")

# Run Go Generate
@generate:
    just step_prefix="Generating" step go generate ./...

# Update vendor directory to match go.mod
@vendor:
    just step_prefix="Tidying" step go mod tidy
    just step_prefix="Vendoring" step go mod vendor

# Run all tests
[group("test")]
test: test-static test-lint test-unit

# Run static checks
[group("test")]
@test-static:
    just step_prefix="Running Static Checks" step \
        {{ staticcheck }} -f stylish $(go list ./... | grep -v '/reference')

# Run lint checks `package:"packages to test, empty for all"`
[group("test")]
@test-lint +package="./...":
    just step_prefix="Formatting Go Code" step go fmt ./...
    just step_prefix="Linting Go Code" step {{ golangci-lint }} run ./cmd/... ./internal/... ./pkg/...
    just step_prefix="Checking Modernize" step go run {{ modernize }} -test -fix $(go list {{ package }} | grep -v '/reference/')

# Run unit tests
[group("test")]
@test-unit:
    just step_prefix="Running Go Tests" step go test -cover ./...

[private]
[script]
banner message:
    if [ -t 1 ]; then
        printf '{{ banner_prefix }}\e[30;43;1m %-80s\e[0m\n\n' '{{ message }}'
    else
        printf '{{ banner_prefix }} %-80s\n\n' '{{ message }}'
    fi

[private]
[script]
step *command:
    tdir=$(mktemp -d)
    trap 'rm -rf "$tdir"' EXIT

    if [ -t 2 ]; then
        printf '\e[34;1m…\e[0m %s' "{{ step_prefix }}" >&2
    else
        printf '… %s' "{{ step_prefix }}" >&2
    fi

    if {{ command }} &> "${tdir}/out.log"; then
        if [ -t 2 ]; then
            printf '\r\e[32;1m%s\e[0m\n' '✔️' >&2
        else
            printf '\r%s\n' '✔️' >&2
        fi
    else
        if [ -t 2 ]; then
            printf '\r\e[31;1m%s\e[0m\n' '❌' >&2
        else
            printf '\r%s\n' '❌' >&2
        fi

        cat "$tdir/out.log" >&2
    	exit 1
    fi

[script]
_build source target="":
    target_name="{{ if target == "" { source } else { target } }}"
    target_path="{{ bins }}/${target_name}"
    source_path="cmd/{{ source }}"
    version=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
    just step_prefix="Building ${target_name}" step "go build -ldflags \"-X github.com/kennyp/speedrun/pkg/version.Version=${version}\" -o \"${target_path}\" \"./${source_path}\"" 
    just step_prefix="Signing ${target_name}" step just sign "${target_path}"

[private]
[macos]
@sign binary:
    codesign -v {{ binary }} >&/dev/null || codesign -s - {{ binary }}

[private]
[linux]
@sign binary:
    printf ""
