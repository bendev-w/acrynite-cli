# Install acrynite: irm https://raw.githubusercontent.com/bendev-w/acrynite-cli/main/install.ps1 | iex
$ErrorActionPreference = 'Stop'

$repo = 'bendev-w/acrynite-cli'
$version = if ($env:ACRYNITE_VERSION) { $env:ACRYNITE_VERSION } else { 'latest' }
$installDir = if ($env:ACRYNITE_INSTALL_DIR) { $env:ACRYNITE_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'acrynite\bin' }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

if ($version -eq 'latest') {
    $version = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name
}
$num = $version.TrimStart('v')

$archive = "acrynite_${num}_windows_${arch}.zip"
$base = "https://github.com/$repo/releases/download/$version"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Write-Host "Downloading $archive ..."
    Invoke-WebRequest "$base/$archive" -OutFile (Join-Path $tmp $archive)
    Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match "\s$([regex]::Escape($archive))$" } | Select-Object -First 1
    if (-not $line) { throw 'Checksum entry not found' }
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash (Join-Path $tmp $archive) -Algorithm SHA256).Hash
    if ($expected -ne $actual.ToLower()) { throw 'Checksum verification failed' }

    Expand-Archive (Join-Path $tmp $archive) -DestinationPath $tmp -Force
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-Item (Join-Path $tmp 'acrynite.exe') (Join-Path $installDir 'acrynite.exe') -Force
}
finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $installDir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$installDir", 'User')
    Write-Host "Added $installDir to your PATH. Restart your terminal to use 'acrynite'."
}

Write-Host "Installed acrynite $version to $installDir"
