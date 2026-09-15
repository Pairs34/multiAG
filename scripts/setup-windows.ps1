param(
    [Parameter(Mandatory = $true)]
    [string]$Router,

    [ValidateSet('openai', 'native')]
    [string]$WireFormat = 'openai',

    [string]$Settings,
    [string]$ApiKeyFile,
    [string]$StateDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$projectRoot = Split-Path -Parent $PSScriptRoot
$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) {
    throw 'Python 3.10 or newer was not found on PATH.'
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go 1.23 or newer was not found on PATH.'
}

Push-Location $projectRoot
try {
    New-Item -ItemType Directory -Force -Path 'bin' | Out-Null
    & go build -trimpath -buildvcs=false -o 'bin/agrouter.exe' './cmd/agrouter'
    if ($LASTEXITCODE -ne 0) {
        throw 'agrouter.exe build failed.'
    }

    $controllerArgs = @(
        'scripts/router_trial.py', 'setup',
        '--router', $Router,
        '--wire-format', $WireFormat
    )
    if ($Settings) {
        $controllerArgs += @('--settings', $Settings)
    }
    if ($ApiKeyFile) {
        $controllerArgs += @('--api-key-file', $ApiKeyFile)
    }
    if ($StateDir) {
        $controllerArgs += @('--state-dir', $StateDir)
    }
    & $python.Source @controllerArgs
    if ($LASTEXITCODE -ne 0) {
        throw 'Windows bridge setup failed.'
    }
} finally {
    Pop-Location
}
