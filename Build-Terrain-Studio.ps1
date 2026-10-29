param([string]$OutputDirectory = "dist")

$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "Go tests failed; build stopped." }
    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
    $executable = Join-Path $OutputDirectory "Terrain-Studio.exe"
    go build -trimpath -tags production -ldflags "-s -w -H windowsgui" -o $executable .
    if ($LASTEXITCODE -ne 0) { throw "Windows build failed." }
    Copy-Item "Start-Terrain-Studio.cmd", "README.md", "NOTICE" $OutputDirectory -Force
    $documentation = Join-Path $OutputDirectory "docs"
    New-Item -ItemType Directory -Force -Path $documentation | Out-Null
    Copy-Item "docs/report-audit.md", "docs/validation.md", "docs/contour-redesign.md" $documentation -Force
    $checksum = (Get-FileHash -Algorithm SHA256 $executable).Hash.ToLowerInvariant()
    "$checksum  Terrain-Studio.exe" | Set-Content (Join-Path $OutputDirectory "SHA256SUMS.txt") -Encoding ascii
    Write-Host "Built $executable. Windows WebView2 is required to run the desktop interface."
} finally {
    Pop-Location
}
