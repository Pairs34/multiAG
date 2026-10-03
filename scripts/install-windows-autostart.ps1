param(
    [switch]$Remove
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$projectRoot = Split-Path -Parent $PSScriptRoot
$startup = [Environment]::GetFolderPath('Startup')
$launcher = Join-Path $startup 'multiag-router.vbs'

if ($Remove) {
    Remove-Item -LiteralPath $launcher -ErrorAction SilentlyContinue
    Write-Host "Removed $launcher"
    return
}

$python = (Get-Command python -ErrorAction Stop).Source
$controller = Join-Path $projectRoot 'scripts\router_trial.py'
if (-not (Test-Path (Join-Path $projectRoot '.local\router-trial\state.json'))) {
    throw 'Run scripts\setup-windows.ps1 first.'
}

# A .vbs launcher starts the controller without a console window at logon.
$command = '"' + $python + '" "' + $controller + '" restart'
$vbs = 'CreateObject("WScript.Shell").Run "' + ($command -replace '"', '""') + '", 0, False'
Set-Content -LiteralPath $launcher -Value $vbs -Encoding ASCII
Write-Host "Installed $launcher"
