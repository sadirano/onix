# Behavioral verification of the generated .exe wrappers (o, e, r, ...).
#
# Unlike the unit tests, which check installation and string-match the
# snippet, this script executes the installed wrappers by bare name under
# cmd.exe — the only way to catch argv[0] dispatch, PATH resolution, and the
# navigation subshell wiring end to end.
#
# Runs against a throwaway $env:ONIX_HOME with the sandbox bin dir at the head
# of PATH. Navigation opens a fresh shell rooted at the target, so ONIX_SHELL
# points at a stub that prints "NAV:<cwd>" and exits — that makes navigation
# observable and non-interactive (a real ComSpec subshell would hang). <nul on
# every call is a belt-and-braces hang guard.
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
# Neutralise any editor launch: a single-executable "editor" that exits
# immediately (EDITOR is exec'd as one binary, so it can't carry arguments).
$editStub = Join-Path $tmp 'editstub.cmd'
Set-Content -Path $editStub -Value "@echo off`r`nexit /b 0" -NoNewline
$env:EDITOR = $editStub

& $exe --init --skip-profile | Out-Null
$bin = Join-Path $tmp 'bin'

# The wrappers are installed as .exe hardlinks/copies of onix.
foreach ($w in 'o', 'e', 'r', 'p', 'y', 's', 'sg', 'ff') {
    if (-not (Test-Path (Join-Path $bin "$w.exe"))) { throw "wrapper $w.exe not installed" }
}

# Navigation stub: print the working dir the subshell opened in, then exit.
$navStub = Join-Path $tmp 'navstub.cmd'
Set-Content -Path $navStub -Value "@echo off`r`necho NAV:%CD%" -NoNewline
$env:ONIX_SHELL = $navStub

# Picker toolchain stubs: es prints a fake candidate; fzf-cancel prints nothing
# (Esc); fzf-pick prints a real directory. The cancel stubs always precede the
# real PATH so no test can reach the real interactive fzf.
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

# --- A. o known alias: resolves and opens the subshell rooted at the dir.
& $exe demo $env:TEMP *> $null
$out = cmd /c "set PATH=$bin;%PATH%&& o demo <nul 2>&1"
$code = $LASTEXITCODE
$navDir = ($out | Where-Object { $_ -match '^NAV:' }) -replace '^NAV:', ''
$wantDemo = (Get-Item $env:TEMP).FullName
Check "A: o known alias navigates (exit=$code)" `
    ($code -eq 0 -and $navDir -and ((Get-Item $navDir).FullName -ieq $wantDemo)) "nav='$navDir' want='$wantDemo' :: $($out -join '|')"

# --- B. o unknown alias, es missing: picker bails, nothing registered.
$out = cmd /c "set PATH=$bin;C:\Windows\System32&& o aliasb <nul 2>&1"
$code = $LASTEXITCODE
$names = & $exe --list-names
Check "B: o unknown bails when es missing (exit=$code)" `
    ($code -eq 1 -and ($out -join ' ') -match 'Everything') ($out -join '|')
Check "B2: nothing registered after missing-es bail" ($names -notcontains 'aliasb') ($names -join ',')

# --- C. o unknown alias, fzf cancel: bails, registers nothing, no navigation.
$out = cmd /c "set PATH=$bin;$($stubCancel.FullName);C:\Windows\System32&& o aliasc <nul 2>&1"
$code = $LASTEXITCODE
$names = & $exe --list-names
Check "C: o unknown bails on cancelled pick (exit=$code)" `
    ($code -eq 1 -and ($out -join ' ') -notmatch 'NAV:') ($out -join '|')
Check "C2: nothing registered after cancelled pick" ($names -notcontains 'aliasc') ($names -join ',')

# --- D. o unknown alias, real pick: registers the alias and navigates into it.
$out = cmd /c "set PATH=$bin;$($stubPick.FullName);C:\Windows\System32&& o aliasd <nul 2>&1"
$code = $LASTEXITCODE
$names = & $exe --list-names
$navDir = ($out | Where-Object { $_ -match '^NAV:' }) -replace '^NAV:', ''
Check "D: o unknown registers + navigates on real pick (exit=$code)" `
    ($code -eq 0 -and $navDir -and ((Get-Item $navDir).FullName -ieq $picked.FullName)) "nav='$navDir' want='$($picked.FullName)' :: $($out -join '|')"
Check "D2: alias registered after pick" ($names -contains 'aliasd') ($names -join ',')

# --- E. action wrappers desugar: e (edit) and r (run) exit cleanly.
$out = cmd /c "set PATH=$bin;%PATH%&& e demo <nul 2>&1"
Check "E: e (edit) wrapper runs (exit=$LASTEXITCODE)" ($LASTEXITCODE -eq 0) ($out -join '|')
$out = cmd /c "set PATH=$bin;%PATH%&& r demo cmd /c ""echo ran"" <nul 2>&1"
$code = $LASTEXITCODE
Check "F: r (run) wrapper executes in alias dir (exit=$code)" `
    ($code -eq 0 -and ($out -join ' ') -match 'ran') ($out -join '|')

# --- G. no .cmd wrappers should be generated any more.
$cmds = Get-ChildItem -Path $bin -Filter '*.cmd' -ErrorAction SilentlyContinue
Check "G: no .cmd wrappers remain" ($null -eq $cmds -or $cmds.Count -eq 0) (($cmds | ForEach-Object Name) -join ',')

Remove-Item -Recurse -Force $tmp
if ($script:fail) { Write-Host "`nRESULT: FAILURES"; exit 1 }
Write-Host "`nRESULT: all behavioral checks passed"
exit 0
