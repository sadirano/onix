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
    $segs = Join-Path $env:ONIX_HOME 'segments.toml'
    @'
version = 3

[[contexts]]
segment = "docs"
source-template = "/documentation"

[[contexts]]
segment = "src"
source-template = "/source"

[[contexts]]
segment = "tasks"
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

# M3 — plugin install end-to-end. We synthesize a tiny "probe" plugin in
# a temp git repo so the test is self-contained: no external repo
# dependency, no chance of a transient upstream bug derailing the smoke.
# The probe is intentionally rich — it has a multi-entry onix.toml so we
# exercise both the main wrapper and entry wrappers, and its main.go
# dumps every ONIX_* env var so we can assert the runtime contract.
$probeDir = Join-Path $env:TEMP "onix-probe-$([guid]::NewGuid().Guid.Substring(0,8))"
New-Item -ItemType Directory -Force -Path $probeDir | Out-Null

@'
package main

import (
	"fmt"
	"os"
)

// Probe plugin used by the onix smoke test. Prints the ONIX_* env vars
// and its argv so the test can assert the runtime contract reached the
// plugin intact. Deliberately small — same shape as a real plugin would
// have, minus the actual logic.
func main() {
	fmt.Printf("target=%s\n", os.Getenv("ONIX_TARGET"))
	fmt.Printf("alias=%s\n", os.Getenv("ONIX_ALIAS"))
	fmt.Printf("home=%s\n", os.Getenv("ONIX_HOME"))
	fmt.Printf("module=%s\n", os.Getenv("ONIX_MODULE"))
	fmt.Printf("entry=%s\n", os.Getenv("ONIX_ENTRY"))
	fmt.Printf("config=%s\n", os.Getenv("ONIX_MODULE_CONFIG"))
	fmt.Printf("args=%v\n", os.Args[1:])
}
'@ | Set-Content -Path (Join-Path $probeDir 'main.go')

@'
module onix-probe

go 1.23
'@ | Set-Content -Path (Join-Path $probeDir 'go.mod')

@'
# Two entries to exercise the multi-entry wrapper path: one with a `cmd`
# override (so we test the rename feature), one without.
[[entry]]
name = "ping"

[[entry]]
name = "pong"
cmd = "p-pong"
'@ | Set-Content -Path (Join-Path $probeDir 'onix.toml')

Push-Location $probeDir
try {
    & git init -q
    & git add .
    & git -c user.email=smoke@onix -c user.name=smoke commit -q -m "probe"
    if ($LASTEXITCODE -ne 0) { throw "could not init probe git repo" }
} finally {
    Pop-Location
}

Step "plugin-add" {
    # --unpinned skips the SHA requirement (we're using a local source).
    # --yes skips the confirmation prompt. --name avoids colliding with
    # any user's real `onix-probe` install.
    & $exe plugin add $probeDir --unpinned --yes --name probe
    if ($LASTEXITCODE -ne 0) { throw "plugin add failed" }

    # plugins.toml must record the new plugin. go-toml's marshaller uses
    # single-quoted strings, so match either quote style.
    $plugins = Get-Content (Join-Path $env:ONIX_HOME 'plugins.toml') -Raw
    if ($plugins -notmatch "name = ['""]probe['""]") {
        throw "plugins.toml missing probe plugin"
    }

    # Snippet must have the main wrapper plus per-entry wrappers. The
    # entry "pong" was given `cmd = "p-pong"`, so its wrapper name uses
    # the cmd override — a regression guard for the cmd rename feature.
    $snippet = Get-Content (Join-Path $env:ONIX_HOME 'shell\onix.ps1') -Raw
    if ($snippet -notmatch 'function global:probe') {
        throw "snippet missing main wrapper 'function global:probe'"
    }
    if ($snippet -notmatch 'function global:ping') {
        throw "snippet missing entry wrapper 'function global:ping'"
    }
    if ($snippet -notmatch 'function global:p-pong') {
        throw "snippet missing entry wrapper 'function global:p-pong' (cmd override)"
    }
    # The ping wrapper invokes the alias-flag form `<alias> --plugin probe:ping`.
    if ($snippet -notmatch '\$Alias --plugin probe:ping') {
        throw "ping wrapper does not pass entry=ping to --plugin"
    }
    # The CommandName list is alphabetically sorted in the snippet, so
    # check each wrapper name individually rather than enforcing an order.
    if ($snippet -notmatch '-CommandName [^\r\n]*\bprobe\b') {
        throw "snippet completer missing probe"
    }
    if ($snippet -notmatch '-CommandName [^\r\n]*\bping\b') {
        throw "snippet completer missing ping"
    }
    if ($snippet -notmatch '-CommandName [^\r\n]*\bp-pong\b') {
        throw "snippet completer missing p-pong"
    }
}

Step "plugin-exec" {
    # The probe binary prints every ONIX_* env var. We invoke it via the
    # alias-flag form `onix <alias> -p <plugin>:<entry>` and verify the
    # values flow through correctly.
    $out = & $exe demo -p probe:ping -- foo bar
    if ($LASTEXITCODE -ne 0) { throw "plugin-exec failed" }
    $text = $out -join "`n"
    if ($text -notmatch 'alias=demo') {
        throw "plugin did not see ONIX_ALIAS=demo (got: $text)"
    }
    if ($text -notmatch 'entry=ping') {
        throw "plugin did not see ONIX_ENTRY=ping (got: $text)"
    }
    if ($text -notmatch 'module=probe') {
        throw "plugin did not see ONIX_MODULE=probe (got: $text)"
    }
    if ($text -notmatch 'args=\[foo bar\]') {
        throw "plugin did not receive extras correctly (got: $text)"
    }
}

Step "plugin-list" {
    # Plugin management remains the only kong subtree.
    $out = & $exe plugin list
    if ($LASTEXITCODE -ne 0) { throw "plugin list failed" }
    if (($out -join "`n") -notmatch '\bprobe\b') {
        throw "plugin list missing probe"
    }
}

Step "plugin-doctor" {
    # Doctor must surface the unpinned warning (we installed with
    # --unpinned) and confirm the binary exists.
    $out = & $exe --doctor 2>&1
    $LASTEXITCODE = 0
    $text = $out -join "`n"
    if ($text -notmatch 'plugin:probe') {
        throw "doctor did not report plugin:probe (output: $text)"
    }
    if ($text -notmatch 'UNPINNED') {
        throw "doctor did not flag the plugin as UNPINNED"
    }
}

Step "plugin-remove" {
    & $exe plugin remove probe
    if ($LASTEXITCODE -ne 0) { throw "plugin remove failed" }

    $snippet = Get-Content (Join-Path $env:ONIX_HOME 'shell\onix.ps1') -Raw
    if ($snippet -match 'function global:probe') {
        throw "snippet still references probe after remove"
    }
    if ($snippet -match 'function global:ping') {
        throw "snippet still references ping after remove"
    }
}

Remove-Item -Recurse -Force $probeDir

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
