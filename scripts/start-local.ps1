<#
.SYNOPSIS
  Starts the platform on this machine: the database, the server and, with
  -Tutoring, LiveKit and the tutoring service. Stop it all with stop-local.ps1.

.DESCRIPTION
  Everything runs in the background with no console windows (a selected
  console window freezes the program writing to it). Logs go to local\logs.
  The server and the tutoring service are rebuilt each time (Go caches, so
  this is quick); the frontend is built only when it is missing or with -Build.
  Running it again restarts the server and the tutoring service.

.EXAMPLE
  .\scripts\start-local.ps1
  .\scripts\start-local.ps1 -Build -Tutoring
  .\scripts\start-local.ps1 -Database qp_dev
  .\scripts\start-local.ps1 -Port 8180   # when Windows has reserved 8080 (netsh ... show excludedportrange)
  .\scripts\start-local.ps1 -Port 8180 -Tutoring -TutorPort 8190
#>
param(
	# The database the app uses; created if missing.
	[string]$Database = 'qp_demo',
	# Rebuild the frontend too.
	[switch]$Build,
	# Also start LiveKit and the tutoring service (needs the files in local\, see HOW-TO-RUN.local.md).
	[switch]$Tutoring,
	# The server's port. Windows sometimes reserves a range that includes 8080 after a restart.
	[int]$Port = 8080,
	# The tutoring service's port, with -Tutoring (the same reserved range can include 8090).
	[int]$TutorPort = 8090
)
$ErrorActionPreference = 'Stop'
$PSBoundParameters_Main = $PSBoundParameters # which ports were chosen, for Select-Port
$root = Split-Path -Parent $PSScriptRoot
$bin = Join-Path $root 'backend\bin'
$localDir = Join-Path $root 'local'
$logs = Join-Path $localDir 'logs'
New-Item -ItemType Directory -Force $bin, $logs | Out-Null

function Test-Port([int]$port) {
	$c = New-Object System.Net.Sockets.TcpClient
	try { return $c.ConnectAsync('127.0.0.1', $port).Wait(300) -and $c.Connected } catch { return $false } finally { $c.Dispose() }
}

# Waits for $check, by the clock (a refused connection can take 2 s on
# Windows), and gives up at once if the program has already exited, showing
# the end of its logs.
function Wait-Until([scriptblock]$check, [int]$seconds, [string]$what, $started) {
	$clock = [Diagnostics.Stopwatch]::StartNew()
	while ($clock.Elapsed.TotalSeconds -lt $seconds) {
		if (& $check) { return }
		if ($started.Process.HasExited) {
			Write-Host "$what stopped. The end of its logs:" -ForegroundColor Red
			foreach ($f in $started.Out, $started.Err) { if (Test-Path $f) { Get-Content $f -Tail 5 | ForEach-Object { Write-Host "   $_" } } }
			throw "$what stopped. Logs: $($started.Out), $($started.Err)"
		}
		Start-Sleep -Milliseconds 250
	}
	throw "$what did not start in $seconds s. Logs: $($started.Out), $($started.Err)"
}

# Whether this computer lets a program listen on the port: Windows reserves
# ranges of ports (they can move after a restart), and another program may hold it.
function Test-Free([int]$port) {
	$l = $null
	try { $l = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $port); $l.Start(); return $true }
	catch { return $false }
	finally { if ($l) { $l.Stop() } }
}

# The port to use: the one asked for, or, when it wasn't chosen and isn't
# free, the first free one of a few others.
function Select-Port([int]$port, [string]$param, [int[]]$others) {
	if (Test-Free $port) { return $port }
	$why = "port $port is taken or reserved by Windows (netsh interface ipv4 show excludedportrange protocol=tcp)"
	if ($PSBoundParameters_Main.ContainsKey($param)) { throw "The $why. Choose another with -$param." }
	foreach ($p in $others) {
		if (Test-Free $p) {
			Write-Host "-> $why; using $p instead (or choose with -$param)" -ForegroundColor Yellow
			return $p
		}
	}
	throw "The $why, and so are $($others -join ', '). Choose a free port with -$param."
}

function Test-Health([string]$url) {
	try { return (Invoke-WebRequest $url -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200 } catch { return $false }
}

# Processes started from this repository's binaries, by path.
function Get-Running([string]$exe) {
	Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -and ($_.Path -ieq $exe) }
}

function Start-Background([string]$exe, [string[]]$arguments, [string]$dir, [string]$name) {
	$out = Join-Path $logs "$name.log"
	$err = Join-Path $logs "$name.err.log"
	# Through cmd, which writes the logs: Start-Process's own redirection makes
	# the program inherit this shell's handles, so whatever ran this script
	# (a VS Code task, another script) would wait until the program exits.
	$line = '"' + $exe + '"'
	foreach ($a in $arguments) { $line += ' "' + $a + '"' }
	$line += ' > "' + $out + '" 2> "' + $err + '"'
	$p = Start-Process cmd.exe -ArgumentList '/d', '/c', ('"' + $line + '"') -WorkingDirectory $dir -WindowStyle Hidden -PassThru
	return @{ Process = $p; Out = $out; Err = $err }
}

function Invoke-Step([string]$what, [scriptblock]$block) {
	Write-Host "-> $what"
	& $block
	if ($LASTEXITCODE -ne 0) { throw "$what failed" }
}

# 1. Database: one embedded PostgreSQL 16 on port 54329, shared with the tests.
Push-Location (Join-Path $root 'backend')
try {
	if (Test-Port 54329) {
		Write-Host '-> Database already running on port 54329'
	} else {
		Invoke-Step 'Building the database helper' { go build -o bin\testdb.exe .\cmd\testdb }
		Write-Host '-> Starting the database (the first start downloads PostgreSQL)'
		$log = Start-Background (Join-Path $bin 'testdb.exe') @($Database) (Join-Path $root 'backend') 'database'
		Wait-Until { Test-Port 54329 } 180 'The database' $log
		# testdb creates the database just after PostgreSQL starts listening.
		Start-Sleep -Seconds 2
	}

	# 2. Build.
	$web = Join-Path $root 'frontend'
	if ($Build -or -not (Test-Path (Join-Path $web 'build\index.html'))) {
		Push-Location $web
		try {
			if (-not (Test-Path 'node_modules')) { Invoke-Step 'Installing frontend packages' { npm ci --no-audit --no-fund } }
			Invoke-Step 'Building the frontend' { npm run build }
		} finally { Pop-Location }
	}
	Invoke-Step 'Building the server' { go build -o bin\server.exe .\cmd\server }
	if ($Tutoring) {
		Push-Location (Join-Path $root 'tutoring')
		try { Invoke-Step 'Building the tutoring service' { go build -o ..\backend\bin\tutor.exe .\cmd\tutor } } finally { Pop-Location }
	}
} finally { Pop-Location }

# Restart what's already running, so the new build is used.
$server = Join-Path $bin 'server.exe'
$tutor = Join-Path $bin 'tutor.exe'
$stopped = @(foreach ($exe in @($server, $tutor)) { Get-Running $exe })
$stopped | Stop-Process -Force
# A stopped server can hold its port for a moment.
if ($stopped) { $clock = [Diagnostics.Stopwatch]::StartNew(); while (-not (Test-Free $Port) -and $clock.Elapsed.TotalSeconds -lt 3) { Start-Sleep -Milliseconds 200 } }

# Ports this computer allows now: Windows reserves ranges, which can move after a restart.
$Port = Select-Port $Port 'Port' @(8180, 8280, 8380, 8480, 8580)
if ($Tutoring) { $TutorPort = Select-Port $TutorPort 'TutorPort' (@(8190, 8290, 8390, 8490, 8590) | Where-Object { $_ -ne $Port }) }

$dbUrl = "postgres://postgres:postgres@localhost:54329/$Database`?sslmode=disable"

# 3. Tutoring: LiveKit on the ports in local\livekit.yaml, then the tutoring service.
if ($Tutoring) {
	foreach ($f in 'livekit.yaml', 'livekit-keys.yaml', 'tutoring.secret') {
		if (-not (Test-Path (Join-Path $localDir $f))) { throw "Tutoring needs local\$f (see HOW-TO-RUN.local.md, section 6)" }
	}
	$livekit = Join-Path $root 'loadtest\.bin\livekit-server.exe'
	if (-not (Test-Path $livekit)) { throw 'LiveKit is missing: run "node loadtest/tutoring-media.mjs" once to download it' }
	$lkConf = Get-Content (Join-Path $localDir 'livekit.yaml')
	$lkPort = [int](($lkConf | Select-String '^port:\s*(\d+)' | Select-Object -First 1).Matches[0].Groups[1].Value)
	$lkTcp = ($lkConf | Select-String '^\s+tcp_port:\s*(\d+)' | Select-Object -First 1)
	if (Test-Port $lkPort) {
		Write-Host "-> LiveKit already running on port $lkPort"
	} else {
		# Windows reserves ranges of ports, which can move after a restart: say so, rather than let LiveKit fail.
		$taken = @($lkPort) + @(if ($lkTcp) { [int]$lkTcp.Matches[0].Groups[1].Value }) | Where-Object { -not (Test-Free $_) }
		if ($taken) { throw "LiveKit can't use port $($taken -join ', '): it's taken or reserved by Windows (netsh interface ipv4 show excludedportrange protocol=tcp). Change port, rtc.tcp_port and rtc.udp_port in local\livekit.yaml." }
		Write-Host "-> Starting LiveKit on port $lkPort"
		$log = Start-Background $livekit @('--config', 'livekit.yaml') $localDir 'livekit'
		Wait-Until { Test-Port $lkPort } 30 'LiveKit' $log
	}
	$env:TUTOR_DATABASE_URL = $dbUrl
	$env:TUTOR_SECRET_FILE = Join-Path $localDir 'tutoring.secret'
	$env:TUTOR_PLATFORM_URL = "http://127.0.0.1:$Port"
	$env:TUTOR_PLATFORM_ORIGIN = "http://localhost:$Port"
	$env:LIVEKIT_URL = "http://127.0.0.1:$lkPort"
	$env:LIVEKIT_PUBLIC_URL = "ws://127.0.0.1:$lkPort"
	$env:LIVEKIT_API_KEY = 'tutoring'
	$env:LIVEKIT_API_SECRET_FILE = Join-Path $localDir 'livekit-keys.yaml'
	$env:TUTOR_LISTEN = "127.0.0.1:$TutorPort"
	$env:TUTOR_RECORDINGS_DIR = Join-Path $localDir 'recordings'
	# Measured bandwidth and CPU (D-82), when livekit.yaml sets prometheus: port: (or the older prometheus_port).
	$prom = Select-String -Path (Join-Path $localDir 'livekit.yaml') -Pattern '^(?:prometheus_port:|\s+port:)\s*(\d+)' -Context 1, 0 |
		Where-Object { $_.Line -match '^prometheus_port' -or $_.Context.PreContext[0] -match '^prometheus:' } | Select-Object -First 1
	$env:LIVEKIT_METRICS_URL = if ($prom) { "http://127.0.0.1:$($prom.Matches[0].Groups[1].Value)/metrics" } else { '' }
	Write-Host '-> Starting the tutoring service'
	$log = Start-Background $tutor $null (Join-Path $root 'tutoring') 'tutoring'
	Wait-Until { Test-Health "http://127.0.0.1:$TutorPort/healthz" } 30 'The tutoring service' $log
	$env:QP_TUTORING_URL = "http://localhost:$TutorPort"
	$env:QP_TUTORING_SECRET_FILE = Join-Path $localDir 'tutoring.secret'
}

# 4. The server. It applies migrations on start; a missing master key is created (local\kek.key).
$kek = Join-Path $localDir 'kek.key'
if (-not (Test-Path $kek)) { $env:QP_KEK_GENERATE = '1' }
$env:QP_DATABASE_URL = $dbUrl
$env:QP_KEK_FILE = $kek
$env:QP_LISTEN = "127.0.0.1:$Port"
$env:QP_BASE_URL = "http://localhost:$Port"
$env:QP_STATIC_DIR = Join-Path $root 'frontend\build'
$env:QP_DEV = '1'
Write-Host '-> Starting the server'
$log = Start-Background $server $null (Join-Path $root 'backend') 'server'
Wait-Until { Test-Health "http://127.0.0.1:$Port/healthz" } 60 'The server' $log

Write-Host ''
Write-Host "Running. Database: $Database" -ForegroundColor Green
Write-Host "  App        http://localhost:$Port"
Write-Host "  Docs       http://localhost:$Port/docs/overview"
Write-Host "  API        http://localhost:$Port/api/docs"
if ($Tutoring) { Write-Host "  Tutoring   http://localhost:$TutorPort/healthz" }
Write-Host "  Logs       $logs"
Write-Host '  Stop with  .\scripts\stop-local.ps1'
