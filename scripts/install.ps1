<#
packnest installer for Windows (never touches GitHub)

  iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex

  -Mirror default|cn   default = gitee release zip (fast in China too, recommended)
                       cn      = npm platform package from npmmirror (@packnest/win32-<arch>)
  -Base URL            any static host base URL (self-hosted OSS/CDN/intranet), highest priority
  -Version vX.Y.Z      exact version
  -Dir DIR             install dir (default: $env:LOCALAPPDATA\packnest\bin)
  -SkipVerify          skip the hash check (not recommended; only when checksums are unreachable)
  -DryRun              print the plan only

NOTE: every user-facing message is ASCII on purpose. This script is executed via
`iwr ... | iex`, which decodes the response with the console codepage; on a GBK/legacy
console a UTF-8 body turns Chinese literals into mojibake. Comments stay Chinese (harmless).
#>
param(
  [ValidateSet('default', 'cn')][string]$Mirror = 'default',
  [string]$Base = $env:PK_INSTALL_BASE,
  [string]$Version = '',
  [string]$Dir = '',
  [switch]$SkipVerify,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # 大文件下载别刷进度条

$GiteeRepo = if ($env:PK_GITEE_REPO) { $env:PK_GITEE_REPO } else { 'null_593_5354/packnest' }
$Registry  = if ($env:PK_NPM_REGISTRY) { $env:PK_NPM_REGISTRY } else { 'https://registry.npmmirror.com' }
$Scope     = if ($env:PK_NPM_SCOPE) { $env:PK_NPM_SCOPE } else { '@packnest' }

if (-not $Dir) {
  $Dir = if ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'packnest\bin' } else { Join-Path $HOME '.packnest\bin' }
}
$TempRoot = if ($env:TEMP) { $env:TEMP } elseif ([IO.Path]::GetTempPath()) { [IO.Path]::GetTempPath() } else { '.' }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'ARM64' { 'arm64' }
  'AMD64' { 'amd64' }
  default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

function Get-LatestVersion {
  if ($Base) {
    $v = (Invoke-WebRequest -Uri "$($Base.TrimEnd('/'))/latest" -TimeoutSec 15 -UseBasicParsing).Content.Trim()
    if ($v) { return $v.TrimStart('v') }
  }
  # npm registry is public and needs no credentials (gitee's API now returns 401 anonymously)
  foreach ($reg in @($Registry, 'https://registry.npmjs.org')) {
    try {
      $meta = Invoke-RestMethod -Uri "$($reg.TrimEnd('/'))/packnest/latest" -TimeoutSec 20
      if ($meta.version) { return $meta.version.TrimStart('v') }
    } catch { }
  }
  try {
    $release = Invoke-RestMethod -Uri "https://gitee.com/api/v5/repos/$GiteeRepo/releases/latest" -TimeoutSec 15
    if ($release.tag_name) { return $release.tag_name.TrimStart('v') }
  } catch { }
  throw 'Cannot detect the latest version; pass -Version explicitly.'
}

if ($env:PK_SKIP_VERIFY -eq '1') { $SkipVerify = [switch]$true }   # `iwr | iex` 传不了开关，用环境变量代替
if (-not $Version -and $env:PK_VERSION) { $Version = $env:PK_VERSION }
if ($env:PK_INSTALL_DIR) { $Dir = $env:PK_INSTALL_DIR }

if (-not $Version) { $Version = Get-LatestVersion }
if (-not $Version) { throw 'Cannot detect the latest version; pass -Version explicitly.' }
$Version = $Version.TrimStart('v')   # allow -Version v0.7.0

# channel: explicit -Base > -Mirror cn (npm platform package) > gitee release
$from = if ($Base) { 'base' } elseif ($Mirror -eq 'cn') { 'npm' } else { 'gitee' }

$tmp = New-Item -ItemType Directory -Path (Join-Path $TempRoot ("pk-" + [guid]::NewGuid())) -Force
try {
  $final = $null   # path of the extracted packnest.exe we will install

  if ($from -eq 'npm') {
    $pkg = "$Scope/win32-$arch"
    $encoded = $pkg -replace '/', '%2F'
    $meta = Invoke-RestMethod -Uri "$($Registry.TrimEnd('/'))/$encoded" -TimeoutSec 30
    $ver = if ($Version) { $Version } else { $meta.'dist-tags'.latest }
    $dist = $meta.versions.$ver.dist
    if (-not $dist) { throw "$pkg has no version $ver" }

    Write-Host "Installing packnest $ver (windows-$arch, npm platform package $pkg)"
    Write-Host "  source: $($dist.tarball)"
    Write-Host "  target: $Dir"
    if ($DryRun) { return }

    $tgz = Join-Path $tmp.FullName 'pkg.tgz'
    Invoke-WebRequest -Uri $dist.tarball -OutFile $tgz -TimeoutSec 300

    if (-not $SkipVerify) {
      $want = $dist.shasum
      if ($want) {
        $got = (Get-FileHash -Path $tgz -Algorithm SHA1).Hash
        if ($got -ne $want.ToUpper()) {
          throw "SHA1 mismatch (registry says $want, got $got). Re-run with -SkipVerify only if you trust the source."
        }
        Write-Host '  verify: sha1 OK (registry dist.shasum)'
      } else {
        Write-Warning 'No shasum in registry metadata; skipping verification (not recommended).'
      }
    }

    $extract = Join-Path $tmp.FullName 'npmpkg'
    New-Item -ItemType Directory -Path $extract -Force | Out-Null
    & tar.exe -xzf $tgz -C $extract
    if ($LASTEXITCODE -ne 0) { throw "Extraction failed (tar.exe exit code $LASTEXITCODE)" }
    $final = Join-Path $extract 'package\bin\packnest.exe'
    if (-not (Test-Path $final)) { throw 'Platform package does not contain package/bin/packnest.exe' }
  } else {
    $name = "packnest_${Version}_windows_${arch}"
    $url = if ($from -eq 'base') {
      "$($Base.TrimEnd('/'))/v${Version}/${name}.zip"
    } else {
      "https://gitee.com/$GiteeRepo/releases/download/v${Version}/${name}.zip"
    }
    $sumsUrl = if ($Base) {
      "$($Base.TrimEnd('/'))/v${Version}/checksums.txt"
    } else {
      "https://gitee.com/$GiteeRepo/releases/download/v${Version}/checksums.txt"
    }

    Write-Host "Installing packnest $Version (windows-$arch)"
    Write-Host "  source: $url"
    Write-Host "  target: $Dir"
    if ($DryRun) { return }

    $zip = Join-Path $tmp.FullName 'pk.zip'
    Invoke-WebRequest -Uri $url -OutFile $zip -TimeoutSec 300

    if (-not $SkipVerify) {
      $sums = try {
        (Invoke-WebRequest -Uri $sumsUrl -TimeoutSec 60 -UseBasicParsing).Content
      } catch { $null }
      if ($null -eq $sums) { $sums = '' }
      $want = $null
      foreach ($line in ($sums -split "`n")) {
        $f = $line.Trim() -split '\s+'
        if ($f.Count -ge 2 -and $f[1] -eq "${name}.zip") { $want = $f[0]; break }
      }
      if (-not $want) {
        throw "No sha256 for ${name}.zip in $sumsUrl. Re-run with -SkipVerify only if you trust the source."
      }
      $got = (Get-FileHash -Path $zip -Algorithm SHA256).Hash
      if ($got -ne $want.ToUpper()) {
        throw "SHA256 mismatch (expected $want, got $got). The artifact may be tampered with or truncated."
      }
      Write-Host '  verify: sha256 OK (checksums.txt)'
    }

    # 解到独立子目录：zip 里的 packnest.exe 不能和 pk.zip 混在一层，
    # 否则下面的复制会和源文件同一路径（0.7.0 在 Windows 上就是这么炸的）。
    $extract = Join-Path $tmp.FullName 'unz'
    New-Item -ItemType Directory -Path $extract -Force | Out-Null
    Expand-Archive -Path $zip -DestinationPath $extract -Force
    $final = Join-Path $extract 'packnest.exe'
    if (-not (Test-Path $final)) { throw 'The zip does not contain packnest.exe' }
  }

  New-Item -ItemType Directory -Path $Dir -Force | Out-Null
  Copy-Item $final (Join-Path $Dir 'packnest.exe') -Force
  # pk 与 packnest 指向同一个二进制（Windows 没有可靠的 symlink，直接复制一份）
  Copy-Item (Join-Path $Dir 'packnest.exe') (Join-Path $Dir 'pk.exe') -Force

  Write-Host "Installed: $(Join-Path $Dir 'pk.exe')"
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($userPath -notlike "*$Dir*") {
    $newPath = if ($userPath) { "$userPath;$Dir" } else { "$Dir" }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    Write-Host "Added $Dir to the user PATH (open a new terminal to pick it up)."
  }
  Write-Host 'Next: pk install node@22'
} finally {
  Remove-Item $tmp.FullName -Recurse -Force -ErrorAction SilentlyContinue
}
