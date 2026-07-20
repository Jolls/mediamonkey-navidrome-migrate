#!/usr/bin/env pwsh
# Build the migrate app. Windows by default; pass -Linux to cross-compile
# into ./bin/migrate-linux instead.
param(
    [switch]$Linux
)
$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

if ($Linux) {
    $out = "bin/migrate-linux"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
} else {
    $out = "bin/migrate.exe"
}
New-Item -ItemType Directory -Force -Path (Split-Path $out) | Out-Null

Write-Host "building $out ..."
go build -o $out ./cmd/app
if ($LASTEXITCODE -ne 0) {
    throw "go build failed"
}
Write-Host "built $out"
