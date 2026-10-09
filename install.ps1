# mitto Windows PowerShell Installer
# Usage: irm https://raw.githubusercontent.com/Nova-Stark/mittodrop-cli/master/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "Nova-Stark/mittodrop-cli"
$project = "mittodrop"
$binary = "mitto.exe"
$alias = "mittodrop.exe"

function Write-Step ($message) {
    Write-Host "==> " -ForegroundColor Green -NoNewline
    Write-Host $message -ForegroundColor White
}

function Write-Notice ($message) {
    Write-Host "==> " -ForegroundColor Yellow -NoNewline
    Write-Host $message -ForegroundColor Yellow
}

function Write-Err ($message) {
    Write-Host "==> " -ForegroundColor Red -NoNewline
    Write-Host $message -ForegroundColor Red
}

# 1. Detect Architecture
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "ARM64" { "arm64" }
    default { "amd64" }
}

Write-Step "Detected platform: windows/$arch"

# 2. Fetch Latest Release
Write-Step "Fetching latest release information..."
$releaseUrl = "https://api.github.com/repos/$repo/releases/latest"

$latestTag = $null
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $headers = @{ "User-Agent" = "mitto-installer" }
    $releaseJson = Invoke-RestMethod -Uri $releaseUrl -Headers $headers -Method Get
    $latestTag = $releaseJson.tag_name
} catch {
    # Fallback to web redirect if API rate-limited
    try {
        $req = [System.Net.WebRequest]::Create("https://github.com/$repo/releases/latest")
        $req.AllowAutoRedirect = $false
        $resp = $req.GetResponse()
        $location = $resp.GetResponseHeader("Location")
        $resp.Close()
        if ($location) {
            $latestTag = ($location -split "/")[-1]
        }
    } catch {
        # ignored
    }
}

if (-not $latestTag) {
    Write-Err "Could not determine latest release tag for $repo. Please check your connection."
    exit 1
}

$version = $latestTag.TrimStart("v")
Write-Step "Target release: $latestTag (v$version)"

# 3. Download Archive
$archiveName = "${project}_${version}_windows_${arch}.zip"
$downloadUrl = "https://github.com/$repo/releases/download/$latestTag/$archiveName"
$tempDir = Join-Path $env:TEMP ("mitto-install-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
$zipPath = Join-Path $tempDir $archiveName

try {
    Write-Step "Downloading $archiveName..."
    Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath -UseBasicParsing

    Write-Step "Extracting files..."
    Expand-Archive -Path $zipPath -DestinationPath $tempDir -Force

    $extractedBinary = Join-Path $tempDir $binary
    if (-not (Test-Path $extractedBinary)) {
        Write-Err "Binary $binary was not found in the archive."
        exit 1
    }

    # 4. Install into %LOCALAPPDATA%\mitto
    $installDir = Join-Path $env:LOCALAPPDATA "mitto"
    if (-not (Test-Path $installDir)) {
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    }

    $targetPath = Join-Path $installDir $binary
    Copy-Item -Path $extractedBinary -Destination $targetPath -Force

    # Provide alias mittodrop.exe alongside mitto.exe
    $aliasPath = Join-Path $installDir $alias
    Copy-Item -Path $extractedBinary -Destination $aliasPath -Force

    Write-Step "Installed binaries to: $installDir"

    # 5. Add to User PATH if not already present
    $userPath = [Environment]::GetEnvironmentVariable("Path", [EnvironmentVariableTarget]::User)
    $pathEntries = ($userPath -split ";") | Where-Object { $_ -ne "" }

    if ($pathEntries -notcontains $installDir) {
        Write-Step "Adding $installDir to User PATH..."
        $newPath = ($pathEntries + $installDir) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $newPath, [EnvironmentVariableTarget]::User)
    }

    # Update PATH for the current active PowerShell process
    if (($env:Path -split ";") -notcontains $installDir) {
        $env:Path = "$installDir;$env:Path"
    }

    Write-Host ""
    Write-Host "Installation successful!" -ForegroundColor Green
    Write-Host ""
    Write-Host "You can now run:" -ForegroundColor White
    Write-Host "  mitto --help" -ForegroundColor Cyan
    Write-Host "  mitto -v" -ForegroundColor Cyan
    Write-Host ""
    & $targetPath -v
}
finally {
    if (Test-Path $tempDir) {
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
