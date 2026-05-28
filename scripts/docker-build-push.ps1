param(
    [string]$Image = "ghcr.io/shukebta/mediastation-go",
    [string]$Tag = "latest",
    [string]$Platforms = "linux/amd64,linux/arm64",
    [switch]$Load
)

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $false
$Builder = "mediastation-builder"

docker buildx inspect $Builder *> $null
if ($LASTEXITCODE -ne 0) {
    docker buildx create --name $Builder --use | Out-Null
} else {
    docker buildx use $Builder | Out-Null
}

$args = @(
    "buildx", "build",
    "--platform", $Platforms,
    "-t", "$Image`:$Tag"
)

if ($Load) {
    $args += "--load"
} else {
    $args += "--push"
}

$args += "."

Write-Host "Building $Image`:$Tag for $Platforms"
docker @args
