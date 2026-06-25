param(
  [string]$Output = "bin/mediastation-go-protected.exe",
  [switch]$Garble
)

$ErrorActionPreference = "Stop"
$env:CGO_ENABLED = "0"

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Output) | Out-Null

if ($Garble) {
  if (-not (Get-Command garble -ErrorAction SilentlyContinue)) {
    throw "garble is not installed. Run: go install mvdan.cc/garble@latest"
  }
  garble -literals -tiny build -trimpath -ldflags="-s -w -buildid=" -o $Output ./cmd/server
  exit $LASTEXITCODE
}

go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o $Output ./cmd/server
exit $LASTEXITCODE
