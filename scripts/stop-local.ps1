<#
.SYNOPSIS
  Stops what start-local.ps1 started: the server, the tutoring service,
  LiveKit and the database.

.DESCRIPTION
  PostgreSQL is shut down cleanly with pg_ctl. Killing only the database
  helper would leave PostgreSQL running and holding port 54329.

.EXAMPLE
  .\scripts\stop-local.ps1
  .\scripts\stop-local.ps1 -KeepDatabase
#>
param(
	# Leave the database running (for running the tests, say).
	[switch]$KeepDatabase
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$bin = Join-Path $root 'backend\bin'

function Stop-ByPath([string]$exe, [string]$name) {
	$running = @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -and ($_.Path -ieq $exe) })
	if ($running.Count -eq 0) { Write-Host "   $name was not running"; return }
	$running | Stop-Process -Force
	$running | ForEach-Object { $_.WaitForExit(5000) | Out-Null }
	Write-Host "-> Stopped $name"
}

Stop-ByPath (Join-Path $bin 'server.exe') 'the server'
Stop-ByPath (Join-Path $bin 'tutor.exe') 'the tutoring service'
Stop-ByPath (Join-Path $root 'loadtest\.bin\livekit-server.exe') 'LiveKit'

if ($KeepDatabase) {
	Write-Host '   The database is still running (-KeepDatabase)'
	return
}

# The embedded PostgreSQL that cmd/testdb runs (see its CachePath and DataPath).
$pgBase = Join-Path $env:LOCALAPPDATA 'quizplatform-pg'
$pgCtl = Join-Path $pgBase 'runtime\bin\pg_ctl.exe'
$data = Join-Path $pgBase 'data'
if ((Test-Path $pgCtl) -and (Test-Path (Join-Path $data 'postmaster.pid'))) {
	& $pgCtl stop -D $data -m fast -w -t 30 | Out-Null
	if ($LASTEXITCODE -eq 0) { Write-Host '-> Stopped PostgreSQL' } else { Write-Warning "pg_ctl could not stop PostgreSQL (exit $LASTEXITCODE)" }
} else {
	Write-Host '   PostgreSQL was not running'
}
Stop-ByPath (Join-Path $bin 'testdb.exe') 'the database helper'
