# Behavioral verification of the generated o.cmd / register.cmd wrappers.
#
# Unlike the unit tests, which string-match wrapper *content*, this script
# executes the generated scripts under cmd.exe — the only way to catch
# parse-level breakage (e.g. the LF-vs-CRLF read-block bug) and wiring
# mistakes between o.cmd, register.cmd, and the binary.
#
# Runs against a throwaway $env:ONIX_HOME with stub es/fzf so every path is
# deterministic and non-interactive. Wrappers are invoked by bare name with
# the sandbox bin dir at the head of PATH: cmd.exe on hardened systems does
# not resolve commands from the current directory, and bare-name invocation
# keeps %~0 != %~f0 so o.cmd's Win+R `cmd /k` branch never fires. <nul on
# every o.cmd call is a belt-and-braces hang guard regardless.
#
# Usage: pwsh -File scripts/verify-wrappers.ps1
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot

# Build the binary under test.
Push-Location $root
try {
    & go build -trimpath -ldflags="-s -w" -o onix.exe .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}
$exe = Join-Path $root 'onix.exe'

$tmp = New-Item -ItemType Directory -Force -Path (Join-Path $env:TEMP "onix-wrappers-$([guid]::NewGuid().Guid.Substring(0,8))")
$env:ONIX_HOME = $tmp.FullName
# Neutralise any editor launch: an "editor" that exits immediately.
$env:EDITOR = 'cmd /c rem'

& $exe --init --skip-profile | Out-Null
$bin = Join-Path $tmp 'bin'
$last = Join-Path $tmp '.last'

# Stub toolchain: es prints a fake candidate; fzf-cancel prints nothing
# (user pressed Esc); fzf-pick prints a real directory. The cancel stubs
# always precede the real PATH so no test can ever reach the real
# interactive fzf.
$stubCancel = New-Item -ItemType Directory -Force -Path (Join-Path $tmp 'stub-cancel')
$stubPick   = New-Item -ItemType Directory -Force -Path (Join-Path $tmp 'stub-pick')
$picked     = New-Item -ItemType Directory -Force -Path (Join-Path $tmp 'picked-dir')
Set-Content -Path (Join-Path $stubCancel 'es.cmd')  -Value "@echo off`r`necho C:\fake\candidate"
Set-Content -Path (Join-Path $stubCancel 'fzf.cmd') -Value "@echo off`r`nexit /b 130"
Set-Content -Path (Join-Path $stubPick 'es.cmd')    -Value "@echo off`r`necho C:\fake\candidate"
Set-Content -Path (Join-Path $stubPick 'fzf.cmd')   -Value "@echo off`r`necho $($picked.FullName)"

$script:fail = 0
function Check($label, $cond, $detail) {
    if ($cond) { Write-Host "PASS  $label" }
    else { $script:fail = 1; Write-Host "FAIL  $label :: $detail" }
}

# --- A. o.cmd: empty .last (resolve cancelled, @-segment so no picker) must bail.
$out = cmd /c "set PATH=$bin;$($stubCancel.FullName);%PATH%&& call o.cmd mystery@nopealias <nul 2>&1"
$code = $LASTEXITCODE
Check "A: o.cmd bails on empty .last (exit=$code)" `
    ($code -eq 1 -and ($out -join ' ') -match 'nothing to navigate') ($out -join '|')

# --- B. register.cmd: missing Everything es CLI must bail with a message.
$out = cmd /c "set PATH=$bin;C:\Windows\System32&& call register.cmd aliasb 2>&1"
$code = $LASTEXITCODE
Check "B: register.cmd bails when es missing (exit=$code)" `
    ($code -eq 1 -and ($out -join ' ') -match "Everything 'es' CLI not found") ($out -join '|')
$names = & $exe --list-names
Check "B2: nothing registered after missing-es bail" ($names -notcontains 'aliasb') ($names -join ',')

# --- C. register.cmd: fzf cancel (empty pick) must bail and register nothing.
$out = cmd /c "set PATH=$bin;$($stubCancel.FullName);C:\Windows\System32&& call register.cmd aliasc 2>&1"
$code = $LASTEXITCODE
$names = & $exe --list-names
Check "C: register.cmd bails on cancelled pick (exit=$code)" ($code -eq 1) ($out -join '|')
Check "C2: nothing registered after cancelled pick" ($names -notcontains 'aliasc') ($names -join ',')

# --- D. register.cmd: a real pick registers the alias and writes the path to .last.
$out = cmd /c "set PATH=$bin;$($stubPick.FullName);C:\Windows\System32&& call register.cmd aliasd 2>&1"
$code = $LASTEXITCODE
$names = & $exe --list-names
$lastContent = if (Test-Path $last) { (Get-Content $last -TotalCount 1) } else { '' }
Check "D: register.cmd succeeds on real pick (exit=$code)" ($code -eq 0) ($out -join '|')
Check "D2: alias registered after pick" ($names -contains 'aliasd') ($names -join ',')
Check "D3: .last holds picked path for o.cmd to pushd" ($lastContent -eq $picked.FullName) ".last='$lastContent' expected='$($picked.FullName)'"

# --- E. o.cmd full cancel chain: unknown alias -> picker -> Esc -> bail, no registration.
$out = cmd /c "set PATH=$bin;$($stubCancel.FullName);C:\Windows\System32&& call o.cmd aliase <nul 2>&1"
$code = $LASTEXITCODE
$names = & $exe --list-names
Check "E: o.cmd full chain bails on picker cancel (exit=$code)" `
    ($code -eq 1 -and ($out -join ' ') -match 'nothing to navigate') ($out -join '|')
Check "E2: cancelled picker registered nothing" ($names -notcontains 'aliase') ($names -join ',')

# --- F. o.cmd success path: known alias still navigates (exit 0, no bail message).
& $exe demo $env:TEMP *> $null
$out = cmd /c "set PATH=$bin;$($stubCancel.FullName);%PATH%&& call o.cmd demo <nul 2>&1"
$code = $LASTEXITCODE
Check "F: o.cmd known alias still works (exit=$code)" `
    ($code -eq 0 -and ($out -join ' ') -notmatch 'nothing to navigate') ($out -join '|')

# --- G. o.cmd: an unreachable destination (nonexistent drive, so MkdirAll
# fails and .last stays empty) bails instead of pushd-ing nowhere. The
# failed resolve falls through to register.cmd, which hits the cancel stubs.
$usedDrives = (Get-PSDrive -PSProvider FileSystem).Name
$ghostDrive = [char[]](90..72) | Where-Object { "$_" -notin $usedDrives } | Select-Object -First 1
Set-Content -Path (Join-Path $tmp 'aliases.toml') -Value "[ghost]`npath = `"${ghostDrive}:/onix-ghost-test/x`"`n[demo]`npath = `"$($env:TEMP.Replace('\','/'))`""
$out = cmd /c "set PATH=$bin;$($stubCancel.FullName);%PATH%&& call o.cmd ghost <nul 2>&1"
$code = $LASTEXITCODE
Check "G: o.cmd bails on unreachable destination (exit=$code)" `
    ($code -eq 1 -and (($out -join ' ') -match 'cannot enter' -or ($out -join ' ') -match 'nothing to navigate')) ($out -join '|')

Remove-Item -Recurse -Force $tmp
if ($script:fail) { Write-Host "`nRESULT: FAILURES"; exit 1 }
Write-Host "`nRESULT: all behavioral checks passed"
exit 0
