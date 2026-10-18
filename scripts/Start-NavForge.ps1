param(
    [ValidateSet('Replay','Live','Manual')][string]$Mode = 'Replay',
    [string]$Config = 'configs/warehouse.pipegen.json',
    [string]$Metadata = '',
	[string]$Container = '',
    [string]$Video = '',
    [double]$Speed = 1
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
$backend = $null
$frontend = $null
try {
    if ($Container -and $Mode -ne 'Live') { throw '-Container requires -Mode Live' }
    if (-not $Video) {
        $sourceVideo = ''
        if ($env:USERPROFILE) { $sourceVideo = Join-Path $env:USERPROFILE 'Downloads/Camera1.mp4' }
        if ($sourceVideo -and (Test-Path -LiteralPath $sourceVideo)) { $Video = $sourceVideo }
        else { $Video = 'navforge-gpu-deployment/results/output.mp4' }
    }
    foreach ($port in @(8080,3000)) {
        $listener = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
        if ($listener) { throw "Port $port is already in use. Stop the existing NavForge process before starting another session." }
    }
    New-Item -ItemType Directory -Force -Path 'data/logs','bin' | Out-Null
    $env:GOMODCACHE = Join-Path $projectRoot 'data/go-mod'
    $env:GOCACHE = Join-Path $projectRoot 'data/go-cache'
    & go build -o bin/navforge.exe ./cmd/navforge
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    if (-not (Test-Path -LiteralPath 'web/node_modules/next/dist/bin/next')) {
        Push-Location web
        try { & npm ci --cache ../data/npm-cache; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' } } finally { Pop-Location }
    }
    $configPath = (Resolve-Path -LiteralPath $Config).Path
    $backendArgs = @('-config', ('"' + $configPath + '"'))
    if ($Mode -ne 'Manual') {
        if (-not $Metadata) {
            if ($Mode -eq 'Live') { $Metadata = 'data/pipegen-live/metadata.json' }
            else { $Metadata = 'navforge-gpu-deployment/results/metadata.json' }
        }
        $metadataPath = $Metadata
        if (-not $Container) { $metadataPath = [System.IO.Path]::GetFullPath($Metadata) }
        if ($Mode -eq 'Replay' -and -not (Test-Path -LiteralPath $metadataPath)) { throw "Metadata does not exist: $metadataPath" }
        $backendArgs += @('-pipegen-metadata', ('"' + $metadataPath + '"'), '-pipegen-speed', $Speed.ToString([Globalization.CultureInfo]::InvariantCulture))
        if ($Mode -eq 'Live') {
            if (-not $Container -and (Test-Path -LiteralPath $metadataPath)) { throw 'Live mode requires a fresh output path. Choose a new -Metadata path for each GPU run.' }
            $backendArgs += '-pipegen-follow'
            if ($Container) { $backendArgs += @('-pipegen-container', $Container) }
            # Live preview may use the source MP4; growing output files are not served.
        }
    }
    if ($Video) {
        $videoPath = (Resolve-Path -LiteralPath $Video).Path
        $backendArgs += @('-video', ('"' + $videoPath + '"'))
    }
    $backend = Start-Process -FilePath (Join-Path $projectRoot 'bin/navforge.exe') -ArgumentList $backendArgs -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput 'data/logs/backend.out.log' -RedirectStandardError 'data/logs/backend.err.log'
    $nodePath = (Get-Command node).Source
    $frontend = Start-Process -FilePath $nodePath -ArgumentList @('node_modules/next/dist/bin/next','dev','--hostname','127.0.0.1') -WorkingDirectory (Join-Path $projectRoot 'web') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $projectRoot 'data/logs/web.out.log') -RedirectStandardError (Join-Path $projectRoot 'data/logs/web.err.log')
    Write-Host "NavForge $Mode mode: http://127.0.0.1:3000"
    Write-Host 'Logs: data/logs. Press Ctrl+C to stop both services.'
    if ($Mode -eq 'Replay') { Write-Host 'Saved PipeGen inference is replayed at video time; it does not start a new GPU run.' }
    while (-not $backend.HasExited -and -not $frontend.HasExited) { Start-Sleep -Seconds 1 }
    throw 'A NavForge service exited. Check data/logs for details.'
} finally {
    # Next's CLI forks its server. Stop only the process trees this launcher owns.
    if ($frontend -and -not $frontend.HasExited) { & taskkill.exe /PID $frontend.Id /T /F | Out-Null }
    if ($backend -and -not $backend.HasExited) { & taskkill.exe /PID $backend.Id /T /F | Out-Null }
    Pop-Location
}
