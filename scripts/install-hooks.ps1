# Installs the pre-commit git hook.

$gitHooksDir = Join-Path (Get-Item -Path $PSScriptRoot).Parent.FullName ".git", "hooks"
if (-not (Test-Path $gitHooksDir)) {
    Write-Error "Not a git repository or `.git/hooks` directory not found."
    exit 1
}

$hookPath = Join-Path $gitHooksDir "pre-commit"
$hookContent = @"
#!/bin/sh
# Staged Go files
STAGED_GO_FILES=\$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$')

if [ -n "`$STAGED_GO_FILES" ]; then
    # Check if gofumpt is installed
    if ! command -v gofumpt >/dev/null 2>&1; then
        echo "Warning: gofumpt is not installed. Skipping formatting check."
    else
        # Check formatting
        UNFORMATTED_FILES=\$(gofumpt -l `$STAGED_GO_FILES)
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
fi

exit 0
"@

Set-Content -Path $hookPath -Value $hookContent -NoNewline -Encoding utf8
Write-Host "Success: Git pre-commit hook installed to $hookPath"
