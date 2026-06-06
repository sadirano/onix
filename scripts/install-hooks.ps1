# Installs the git hooks: pre-commit (format/vuln/test gate) and
# pre-push (CodeRabbit AI review gate).
#
# NOTE on escaping: this here-string is double-quoted, so PowerShell expands
# $ and $(...). Every shell $ in the generated hooks is escaped as `$ so it
# survives literally into the .sh hook instead of being evaluated here.

$gitHooksDir = Join-Path (Join-Path (Get-Item -Path $PSScriptRoot).Parent.FullName ".git") "hooks"
if (-not (Test-Path $gitHooksDir)) {
    Write-Error "Not a git repository or `.git/hooks` directory not found."
    exit 1
}

# --- pre-commit: gofumpt formatting, govulncheck, and unit tests ------------
$preCommitPath = Join-Path $gitHooksDir "pre-commit"
$preCommitContent = @"
#!/bin/sh
# Staged Go files
STAGED_GO_FILES=`$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$')

if [ -n "`$STAGED_GO_FILES" ]; then
    # Check if gofumpt is installed
    if ! command -v gofumpt >/dev/null 2>&1; then
        echo "Warning: gofumpt is not installed. Skipping formatting check."
    else
        # Check formatting
        UNFORMATTED_FILES=`$(gofumpt -l `$STAGED_GO_FILES)
        if [ -n "`$UNFORMATTED_FILES" ]; then
            echo "Error: The following Go files are not formatted correctly with gofumpt:"
            echo "`$UNFORMATTED_FILES"
            echo "Please run: gofumpt -w <file>"
            exit 1
        fi
    fi

    # Check if govulncheck is installed
    if ! command -v govulncheck >/dev/null 2>&1; then
        echo "Warning: govulncheck is not installed. Skipping vulnerability check."
    else
        # Run vulnerability check
        if ! govulncheck ./...; then
            echo "Error: govulncheck found vulnerabilities. Please fix them before committing."
            exit 1
        fi
    fi

    # Run tests
    echo "Running unit tests..."
    if ! go test ./...; then
        echo "Error: Unit tests failed. Please fix them before committing."
        exit 1
    fi
fi

exit 0
"@

Set-Content -Path $preCommitPath -Value $preCommitContent -NoNewline -Encoding utf8
Write-Host "Success: Git pre-commit hook installed to $preCommitPath"

# --- pre-push: CodeRabbit AI review gate ------------------------------------
# Blocks the push unless `cr review` reports zero findings. Bypass a single
# push with `git push --no-verify`. Requires the CodeRabbit CLI (`cr`) on PATH
# and an authenticated session (`cr auth login`).
$prePushPath = Join-Path $gitHooksDir "pre-push"
$prePushContent = @"
#!/bin/sh
# CodeRabbit AI review gate. Bypass a single push with: git push --no-verify

if ! command -v cr >/dev/null 2>&1; then
    echo "Error: CodeRabbit CLI ('cr') not found on PATH."
    echo "Install it, or bypass once with: git push --no-verify"
    exit 1
fi

# Auth gate: 'cr auth status' exits 0 even when signed out, so key off output.
if cr auth status 2>&1 | grep -qi "not logged in"; then
    echo "Error: CodeRabbit is not authenticated. Run 'cr auth login'."
    echo "Or bypass once with: git push --no-verify"
    exit 1
fi

# cr review exits 0 even when it finds issues, so block off the structured
# findings count instead. --agent emits NDJSON whose final line is
# {"type":"complete","status":"review_completed","findings":N}.
echo "Running CodeRabbit review (cr review --type committed)..."
REVIEW_OUT=`$(cr review --agent --type committed 2>/dev/null)
FINDINGS=`$(printf '%s\n' "`$REVIEW_OUT" | grep '"type":"complete"' | grep -oE '"findings":[0-9]+' | grep -oE '[0-9]+' | tail -1)

if [ -z "`$FINDINGS" ]; then
    echo "Error: CodeRabbit review did not complete (no result emitted)."
    echo "Run 'cr review' manually to diagnose, or bypass once with: git push --no-verify"
    exit 1
fi

if [ "`$FINDINGS" -gt 0 ]; then
    echo "CodeRabbit reported `$FINDINGS finding(s):"
    cr review findings
    echo ""
    echo "Push blocked. Address the findings, or bypass once with: git push --no-verify"
    exit 1
fi

echo "CodeRabbit review passed (0 findings)."
exit 0
"@

Set-Content -Path $prePushPath -Value $prePushContent -NoNewline -Encoding utf8
Write-Host "Success: Git pre-push hook installed to $prePushPath"
