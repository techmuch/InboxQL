# Install InboxQL on Windows.
#
#   irm https://techmuch.github.io/InboxQL/install.ps1 | iex
#
# What it does, in order:
#   1. downloads the release archive and SHA256SUMS
#   2. refuses to go on unless the archive matches its published checksum
#   3. installs iql.exe to %USERPROFILE%\.iql\bin and adds that to your PATH
#   4. `iql setup`           writes %USERPROFILE%\.iql\settings.json and makes your mailbox
#   5. `iql service install` starts InboxQL at logon, with no window
#
# Already installed? This hands over to `iql update`, which backs your mailbox
# up and restarts the service in the right order.
#
# Environment, all optional:
#   INBOXQL_HOME              install somewhere other than %USERPROFILE%\.iql
#   INBOXQL_VERSION           a tag such as v0.1.0, instead of the latest release
#   INBOXQL_NO_SERVICE=1      install and set up, but do not start at logon
#   INBOXQL_DOWNLOAD_BASE     download from here instead of GitHub (testing)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # the progress bar makes downloads many times slower

$Repo = 'techmuch/InboxQL'
$Asset = 'iql-windows-amd64.zip'
$HomeDir = if ($env:INBOXQL_HOME) { $env:INBOXQL_HOME } else { Join-Path $env:USERPROFILE '.iql' }
$BinDir = Join-Path $HomeDir 'bin'
$Bin = Join-Path $BinDir 'iql.exe'

function Fail($message) {
    Write-Host "install: $message" -ForegroundColor Red
    exit 1
}

if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
    Fail "there is no release build for Windows on $($env:PROCESSOR_ARCHITECTURE) yet; build from source: https://github.com/$Repo"
}

# --- already installed: update instead -----------------------------------------

if (Test-Path $Bin) {
    Write-Host "InboxQL is already installed at $Bin; updating it instead."
    & $Bin update --yes
    exit $LASTEXITCODE
}

# --- download and verify ---------------------------------------------------------

if ($env:INBOXQL_DOWNLOAD_BASE) {
    $Base = $env:INBOXQL_DOWNLOAD_BASE
} elseif ($env:INBOXQL_VERSION) {
    $Base = "https://github.com/$Repo/releases/download/$($env:INBOXQL_VERSION)"
} else {
    $Base = "https://github.com/$Repo/releases/latest/download"
}

$Tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("iql-" + [System.Guid]::NewGuid())
New-Item -ItemType Directory -Path $Tmp | Out-Null
try {
    Write-Host "Downloading $Asset"
    $Archive = Join-Path $Tmp $Asset
    $Sums = Join-Path $Tmp 'SHA256SUMS'
    try { Invoke-WebRequest -UseBasicParsing -Uri "$Base/$Asset" -OutFile $Archive }
    catch { Fail "could not download $Base/$Asset" }
    try { Invoke-WebRequest -UseBasicParsing -Uri "$Base/SHA256SUMS" -OutFile $Sums }
    catch { Fail "could not download SHA256SUMS; not installing an unverified binary" }

    $Expected = $null
    foreach ($line in Get-Content $Sums) {
        $fields = $line -split '\s+'
        if ($fields.Count -ge 2 -and ($fields[1] -eq $Asset -or $fields[1] -eq "*$Asset")) {
            $Expected = $fields[0].ToLower()
        }
    }
    if (-not $Expected) { Fail "SHA256SUMS has no entry for $Asset; not installing an unverified binary" }
    $Actual = (Get-FileHash -Algorithm SHA256 $Archive).Hash.ToLower()
    if ($Actual -ne $Expected) { Fail "$Asset does not match its published checksum; not installing it" }
    Write-Host 'Checksum verified'

    Expand-Archive -Path $Archive -DestinationPath $Tmp -Force
    $Extracted = Get-ChildItem -Path $Tmp -Recurse -Filter 'iql.exe' | Select-Object -First 1
    if (-not $Extracted) { Fail 'the archive did not contain iql.exe' }

    # --- install ----------------------------------------------------------------------

    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Move-Item -Force $Extracted.FullName $Bin
    # Downloaded files carry the "came from the internet" mark, which makes
    # SmartScreen ask about an unsigned program every time it starts.
    Unblock-File -Path $Bin
    Write-Host "Installed $Bin"
} finally {
    Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
}

# --- PATH ------------------------------------------------------------------------

$UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($UserPath -split ';') -contains $BinDir)) {
    [Environment]::SetEnvironmentVariable('Path', ($UserPath.TrimEnd(';') + ';' + $BinDir), 'User')
    $env:Path = $env:Path + ';' + $BinDir
    Write-Host "Added $BinDir to your PATH (open a new terminal to use it)"
}

# --- set up and start ----------------------------------------------------------------

& $Bin setup
if ($LASTEXITCODE -ne 0) { Fail 'setup failed' }

if (-not $env:INBOXQL_NO_SERVICE) {
    & $Bin service install
    if ($LASTEXITCODE -ne 0) { Fail 'installing the logon service failed' }
} else {
    Write-Host 'Not installing the logon service (INBOXQL_NO_SERVICE). Start it any time with: iql service install'
}

Write-Host ''
Write-Host 'Done. Instructions, updates and uninstalling: https://techmuch.github.io/InboxQL/'
