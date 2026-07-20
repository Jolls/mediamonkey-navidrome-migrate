#!/usr/bin/env pwsh
# Build the migrate app into ./bin/migrate.exe.
$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$out = "bin/migrate.exe"
New-Item -ItemType Directory -Force -Path (Split-Path $out) | Out-Null

Write-Host "building $out ..."
go build -o $out ./cmd/app
if ($LASTEXITCODE -ne 0) {
    throw "go build failed"
}
Write-Host "built $out"
