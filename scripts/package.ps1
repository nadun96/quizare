<#
.SYNOPSIS
  Builds a release to upload to a Linux server (aaPanel or any other):
  the server for Linux, the frontend, and the install and update scripts,
  in one .tar.gz under dist\.

.DESCRIPTION
  The server needs neither Go nor Node: everything is built here. Upload
  the file, then on the server run install.sh the first time and update.sh
  after that (see the in-app guide "Deploying on aaPanel").

.EXAMPLE
  .\scripts\package.ps1
  .\scripts\package.ps1 -Arch arm64
#>
param(
	# The server's CPU: amd64 (most VPSs) or arm64.
	[ValidateSet('amd64', 'arm64')][string]$Arch = 'amd64'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot

function Invoke-Step([string]$what, [scriptblock]$block) {
	Write-Host "-> $what"
	& $block
	if ($LASTEXITCODE -ne 0) { throw "$what failed" }
}

$version = (git -C $root describe --tags --always --dirty).Trim()
if ($version -like '*-dirty') { Write-Warning "Uncommitted changes are included ($version)." }
$name = "quiz-$version-linux-$Arch"
$dist = Join-Path $root 'dist'
$stage = Join-Path $dist $name
if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
New-Item -ItemType Directory -Force $stage | Out-Null

Push-Location (Join-Path $root 'frontend')
try {
	if (-not (Test-Path 'node_modules')) { Invoke-Step 'Installing frontend packages' { npm ci --no-audit --no-fund } }
	Invoke-Step 'Building the frontend' { npm run build }
} finally { Pop-Location }

Push-Location (Join-Path $root 'backend')
$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
try {
	$env:GOOS = 'linux'; $env:GOARCH = $Arch; $env:CGO_ENABLED = '0'
	Invoke-Step "Building the server for Linux ($Arch)" { go build -trimpath -ldflags '-s -w' -o (Join-Path $stage 'server') ./cmd/server }
} finally {
	foreach ($k in $saved.Keys) { Set-Item "env:$k" $saved[$k] }
	Pop-Location
}

Copy-Item -Recurse (Join-Path $root 'frontend\build') (Join-Path $stage 'frontend')
Copy-Item (Join-Path $root 'deploy\aapanel\*') $stage
# Line endings matter to bash: make sure the scripts have Unix ones.
Get-ChildItem $stage -Filter *.sh | ForEach-Object {
	$text = [IO.File]::ReadAllText($_.FullName) -replace "`r`n", "`n"
	[IO.File]::WriteAllText($_.FullName, $text, (New-Object Text.UTF8Encoding $false))
}
[IO.File]::WriteAllText((Join-Path $stage 'VERSION'), "$version`n")

$tarball = Join-Path $dist "$name.tar.gz"
Invoke-Step 'Packing' { tar -czf $tarball -C $stage . }
Remove-Item -Recurse -Force $stage
$hash = (Get-FileHash $tarball -Algorithm SHA256).Hash.ToLower()
[IO.File]::WriteAllText("$tarball.sha256", "$hash  $name.tar.gz`n")

Write-Host ''
Write-Host "Built $tarball" -ForegroundColor Green
Write-Host "  SHA-256 $hash"
Write-Host '  Upload it to the server, then:'
Write-Host "    first time:  sudo bash install.sh $name.tar.gz   (extract install.sh from it first)"
Write-Host "    updates:     sudo bash /www/quiz-app/update.sh $name.tar.gz"
