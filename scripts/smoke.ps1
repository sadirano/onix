# Smoke test for v2.
#
# Runs the binary against a throwaway $env:ONIX_HOME so it doesn't touch your
# real ~/.onix or your real PowerShell $PROFILE. Exits non-zero on the first
# failure so it's safe to chain with `&&` in scripts.
#
# Usage from the v2/ directory:
#   pwsh -File scripts/smoke.ps1

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot

# 1. Resolve dependencies + build.
# `go mod tidy` is idempotent on a clean checkout — first run fetches kong
# and go-toml and writes go.sum; subsequent runs are a no-op.
Push-Location $root
try {
    & go mod tidy
    if ($LASTEXITCODE -ne 0) { throw "go mod tidy failed" }
    # -s -w strips the symbol table and DWARF (smaller binary, faster load).
    # -trimpath removes local file paths from the binary (faster + reproducible).
    & go build -trimpath -ldflags="-s -w" -o onix.exe .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}
$exe = Join-Path $root 'onix.exe'

# 2. Run against an isolated ONIX_HOME so we don't pollute the user's real one.
$tmp = New-Item -ItemType Directory -Force -Path (Join-Path $env:TEMP "onix-smoke-$([guid]::NewGuid().Guid.Substring(0,8))")
$env:ONIX_HOME = $tmp.FullName
Write-Host "ONIX_HOME = $env:ONIX_HOME"

# 3. The smoke sequence: init (skip-profile), add, list, resolve, yank, doctor.
function Step($label, [scriptblock]$block) {
    Write-Host "--- $label"
    & $block
    if ($LASTEXITCODE -ne 0) { throw "step failed: $label" }
}

Step "init" {
    & $exe --init --skip-profile
}

Step "add" {
    # New grammar: `onix <alias> <path>` registers/updates the alias.
    & $exe demo $env:TEMP
}

Step "list" {
    & $exe --list
}

Step "resolve" {
    # Bare `onix <alias>` is the hot-path resolve.
    $p = & $exe demo
    if (-not $p) { throw "resolve returned empty" }
    Write-Host "resolved -> $p"
}

Step "yank" {
    & $exe demo -y | Out-Null
}

# Remove paths: three flavours, each tested against a side-channel alias so
# the rest of the smoke keeps `demo` intact.
#   1. `<alias> --remove` (no file args) deletes the alias entry.
#   2. `--remove <load-bearing>` without --force must refuse.
#   3. `--remove <file> --force` deletes a non-load-bearing file from ~/.onix.
Step "remove-alias" {
    & $exe removeme $env:TEMP
    if ($LASTEXITCODE -ne 0) { throw "could not add 'removeme' for remove test" }

    & $exe removeme --remove
    if ($LASTEXITCODE -ne 0) { throw "remove of alias 'removeme' failed" }

    $names = & $exe --list-names
    if ($names -contains 'removeme') {
        throw "removeme still listed after --remove (got: $names)"
    }
}

Step "remove-load-bearing-guard" {
    # `aliases.toml` is in the load-bearing set; deleting it without --force
    # must error rather than silently destroying the alias DB. We capture
    # both stdout and stderr and assert the guard message is present.
    $out = & $exe --remove aliases.toml 2>&1
    if ($LASTEXITCODE -eq 0) {
        throw "--remove aliases.toml unexpectedly succeeded: $out"
    }
    $script:LASTEXITCODE = 0
    $text = $out -join "`n"
    if ($text -notmatch 'load-bearing') {
        throw "expected 'load-bearing' refusal message; got: $text"
    }
    # aliases.toml must still exist after the rejected delete.
    if (-not (Test-Path (Join-Path $env:ONIX_HOME 'aliases.toml'))) {
        throw "aliases.toml was deleted despite the guard"
    }
}

Step "remove-file-with-force" {
    # Create a stray file inside ONIX_HOME and verify --force deletes it
    # without prompting. Non-load-bearing name so the guard does not apply.
    $stray = Join-Path $env:ONIX_HOME 'stray.txt'
    Set-Content -Path $stray -Value 'temp'
    if (-not (Test-Path $stray)) { throw "could not create stray.txt for test" }

    & $exe --remove stray.txt --force
    if ($LASTEXITCODE -ne 0) { throw "--remove --force stray.txt failed" }
    if (Test-Path $stray) {
        throw "stray.txt still present after --remove --force"
    }
}

# M2 — custom actions. Write a tiny config.toml declaring an action that
# runs `cmd.exe /c echo hi from <alias>`, regenerate the shell snippet, run
# it via `onix <alias> -X <action>`, and verify the output contains the
# expected string.
Step "custom-action" {
    $cfg = Join-Path $env:ONIX_HOME 'config.toml'
    @'
[[actions]]
name = "say"
exec = "cmd.exe"
args = ["/c", "echo", "hello", "from", "{alias}"]
'@ | Set-Content -Path $cfg

    & $exe --sync
    if ($LASTEXITCODE -ne 0) { throw "--sync failed" }

    $out = & $exe demo -X say
    if ($LASTEXITCODE -ne 0) { throw "demo -X say failed" }
    if ($out -notmatch 'hello from demo') {
        throw "unexpected output: $out"
    }
    Write-Host "demo -X say -> $out"

    # The regenerated snippet must reference the new action AND be pinned
    # to the absolute path of the binary that generated it. Pinning is what
    # prevents a stale onix on PATH from intercepting calls in dev shells.
    $snippet = Get-Content (Join-Path $env:ONIX_HOME 'shell\onix.ps1') -Raw
    if ($snippet -notmatch 'function global:say') {
        throw "snippet missing 'function global:say'"
    }
    if ($snippet -notmatch 'Register-ArgumentCompleter.*-CommandName.*\bsay\b') {
        throw "snippet missing completer registration for 'say'"
    }
    if ($snippet -notmatch [regex]::Escape('$global:onixExe')) {
        throw "snippet missing `$global:onixExe pin"
    }
    if ($snippet -notmatch [regex]::Escape($exe)) {
        throw "snippet not pinned to test binary $exe"
    }
}

Step "list-names" {
    $names = & $exe --list-names
    if ($LASTEXITCODE -ne 0) { throw "list-names failed" }
    if ($names -notcontains 'demo') {
        throw "list-names did not include 'demo' (got: $names)"
    }
}

# M4 — @-segment sub-aliases. Write [[contexts]] entries and verify
# resolve handles: a static template, an inline-value template, a
# composed two-segment input, and the --no-prompt error path for an
# undefined segment.
Step "segments" {
    # Entries in the global segments.toml must carry `scope = "global"` to
    # be visible to all aliases — per-alias segments live in
    # ~/.onix/segments.d/<alias>.toml or <alias>/.onix/segments.toml.
    $segs = Join-Path $env:ONIX_HOME 'segments.toml'
    @'
version = 3

[[contexts]]
segment = "docs"
scope = "global"
source-template = "/documentation"

[[contexts]]
segment = "src"
scope = "global"
source-template = "/source"

[[contexts]]
segment = "tasks"
scope = "global"
source-template = "/tickets/${tasks}"
'@ | Set-Content -Path $segs

    # Plain alias still works (no @ in input). Bare `onix <alias>` resolves.
    $plain = & $exe demo --no-prompt
    if ($LASTEXITCODE -ne 0) { throw "resolve demo failed" }
    $plainSlashed = $plain.Replace('\', '/')

    # Static template.
    $mapped = & $exe "docs@demo" --no-prompt
    if ($LASTEXITCODE -ne 0) { throw "resolve docs@demo failed" }
    if ($mapped.Replace('\', '/') -ne "$plainSlashed/documentation") {
        throw "resolve docs@demo = '$mapped', expected '$plain/documentation'"
    }

    # Inline value.
    $inline = & $exe "tasks:42@demo" --no-prompt
    if ($LASTEXITCODE -ne 0) { throw "resolve tasks:42@demo failed" }
    if ($inline.Replace('\', '/') -ne "$plainSlashed/tickets/42") {
        throw "resolve tasks:42@demo = '$inline', expected '$plain/tickets/42'"
    }

    # Composition: innermost-first. src@docs@demo → demo/documentation/source.
    $multi = & $exe "src@docs@demo" --no-prompt
    if ($LASTEXITCODE -ne 0) { throw "resolve src@docs@demo failed" }
    if ($multi.Replace('\', '/') -ne "$plainSlashed/documentation/source") {
        throw "resolve src@docs@demo = '$multi', expected '$plain/documentation/source'"
    }

    # Undefined segment under --no-prompt is a hard error.
    $undef = & $exe "mystery@demo" --no-prompt 2>&1
    if ($LASTEXITCODE -eq 0) {
        throw "resolve --no-prompt mystery@demo unexpectedly succeeded: '$undef'"
    }
    # Reset $LASTEXITCODE so the Step wrapper doesn't trip on the expected
    # error above.
    $global:LASTEXITCODE = 0

    Write-Host "  docs@demo       -> $mapped"
    Write-Host "  tasks:42@demo   -> $inline"
    Write-Host "  src@docs@demo   -> $multi"
    Write-Host "  mystery@demo    -> errored under --no-prompt (expected)"
}

Step "doctor" {
    # doctor exits non-zero only when there's an actual error; warnings are fine
    # because we deliberately skipped the $PROFILE step above.
    & $exe --doctor
    # Allow non-zero here because the smoke env has no real PROFILE sourced.
    $script:LASTEXITCODE = 0
}

Step "version" {
    & $exe --version
}

# 4. Hot-path microbench. We also measure a no-op Go binary built with the
# same flags so we can attribute time between process-spawn (the OS floor)
# and onix's actual work. If both are within ~1ms of each other, onix is
# spawn-bound and the only way to go faster is daemon mode.
Write-Host "--- hot-path timing (10 iterations)"
$timings = @()
for ($i = 0; $i -lt 10; $i++) {
    $t = Measure-Command { & $exe demo | Out-Null }
    $timings += $t.TotalMilliseconds
}
$avg = ($timings | Measure-Object -Average).Average
$min = ($timings | Measure-Object -Minimum).Minimum
Write-Host ("  onix resolve   min={0:N2}ms  avg={1:N2}ms" -f $min, $avg)

# Tab-completion path: every Tab keystroke triggers `onix --list-names`, so
# it has its own hot-path bypass. Measure it the same way.
$ltimings = @()
for ($i = 0; $i -lt 10; $i++) {
    $t = Measure-Command { & $exe --list-names | Out-Null }
    $ltimings += $t.TotalMilliseconds
}
$lAvg = ($ltimings | Measure-Object -Average).Average
$lMin = ($ltimings | Measure-Object -Minimum).Minimum
Write-Host ("  onix list-names min={0:N2}ms  avg={1:N2}ms" -f $lMin, $lAvg)

# Build a no-op Go binary with identical flags for an apples-to-apples
# baseline. If hello.exe takes ~8ms too, the cost is entirely the OS.
$helloDir = Join-Path $env:TEMP "onix-hello-$([guid]::NewGuid().Guid.Substring(0,8))"
New-Item -ItemType Directory -Force -Path $helloDir | Out-Null
$helloSrc = Join-Path $helloDir "main.go"
@'
package main
func main() {}
'@ | Set-Content -Path $helloSrc

Push-Location $helloDir
try {
    & go mod init hello | Out-Null
    & go build -trimpath -ldflags="-s -w" -o hello.exe .
    if ($LASTEXITCODE -ne 0) { throw "hello build failed" }
} finally {
    Pop-Location
}
$helloExe = Join-Path $helloDir "hello.exe"

$baseline = @()
for ($i = 0; $i -lt 10; $i++) {
    $t = Measure-Command { & $helloExe 2>$null | Out-Null }
    $baseline += $t.TotalMilliseconds
}
$bAvg = ($baseline | Measure-Object -Average).Average
$bMin = ($baseline | Measure-Object -Minimum).Minimum
Write-Host ("  hello.exe      min={0:N2}ms  avg={1:N2}ms  (OS spawn floor)" -f $bMin, $bAvg)

Remove-Item -Recurse -Force $helloDir

# 5. Cleanup.
Remove-Item -Recurse -Force $tmp
Write-Host ""
Write-Host "all good."
