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

# Get-LatestVersion resolves the newest version for the gitee/base channel.
#
# Order matters: ask **gitee** first, because that is where the artifact comes from.
# Asking npm first bit us in the field: gitee had 0.7.6 published while npm's `latest`
# was still 0.7.4 (0.7.5/0.7.6 were staged and not yet approved), so the one-liner
# installed 0.7.4 and looked "broken".
# Download-File fetches $url into $out, surviving flaky networks.
#
# Why this exists: on a real Windows box `Invoke-WebRequest` aborted with
# "remote host forcibly closed the connection" while curl fetched the same 3.2 MB asset fine.
# Two usual causes: Windows PowerShell 5.1 defaults to old TLS, and IWR goes through the
# system proxy (which may reset long transfers to domestic hosts). So each attempt tries
# WebClient-without-proxy first, then IWR, with a short backoff between rounds.
function Download-File {
  param([string]$url, [string]$out, [int]$rounds = 3)
  try { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 -bor [Net.SecurityProtocolType]::Tls11 } catch { }
  $last = $null
  for ($i = 1; $i -le $rounds; $i++) {
    foreach ($mode in @('webclient-noproxy', 'webclient', 'iwr')) {
      try {
        if (Test-Path $out) { Remove-Item $out -Force -ErrorAction SilentlyContinue }
        switch ($mode) {
          'webclient-noproxy' {
            $wc = New-Object System.Net.WebClient
            $wc.Proxy = $null
            $wc.Headers.Add('User-Agent', 'packnest-installer')
            $wc.DownloadFile($url, $out)
          }
          'webclient' {
            $wc = New-Object System.Net.WebClient
            $wc.Headers.Add('User-Agent', 'packnest-installer')
            $wc.DownloadFile($url, $out)
          }
          default {
            Invoke-WebRequest -Uri $url -OutFile $out -UseBasicParsing -MaximumRedirection 10 -TimeoutSec 300
          }
        }
        if ((Test-Path $out) -and ((Get-Item $out).Length -gt 0)) { return }
        $last = "$mode produced an empty file"
      } catch {
        $last = "$mode : $($_.Exception.Message)"
      }
    }
    if ($i -lt $rounds) {
      Write-Host "  download failed ($last); retry $i/$rounds"
      Start-Sleep -Seconds $i
    }
  }
  throw "Download failed after $rounds attempts ($url). Last error: $last"
}

# tar 命令：Windows 10 1803+ 自带 tar.exe（bsdtar）；其它平台用 tar。
function Get-TarCommand {
  foreach ($c in @('tar.exe', 'tar')) {
    $cmd = Get-Command $c -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
  }
  throw 'tar not found: this Windows build cannot extract .tgz; use the gitee zip channel instead.'
}

function Get-LatestVersion {
  if ($Base) {
    try {
      $v = (Invoke-WebRequest -Uri "$($Base.TrimEnd('/'))/latest" -TimeoutSec 15 -UseBasicParsing).Content.Trim()
      if ($v) { return $v.TrimStart('v') }
    } catch { }
  }
  try {
    $release = Invoke-RestMethod -Uri "https://gitee.com/api/v5/repos/$GiteeRepo/releases/latest" -TimeoutSec 15
    if ($release.tag_name) { return $release.tag_name.TrimStart('v') }
  } catch { }
  # gitee's API occasionally answers 401 anonymously: fall back to the single source of truth
  # in the repo (scripts/latest, bumped by `make release` and checked by verify-release).
  try {
    $v = (Invoke-WebRequest -Uri "https://gitee.com/$GiteeRepo/raw/main/scripts/latest" -TimeoutSec 15 -UseBasicParsing).Content.Trim()
    if ($v) { return $v.TrimStart('v') }
  } catch { }
  foreach ($reg in @($Registry, 'https://registry.npmjs.org')) {
    try {
      $meta = Invoke-RestMethod -Uri "$($reg.TrimEnd('/'))/packnest/latest" -TimeoutSec 20
      if ($meta.version) { return $meta.version.TrimStart('v') }
    } catch { }
  }
  throw 'Cannot detect the latest version; pass -Version explicitly.'
}

if ($env:PK_SKIP_VERIFY -eq '1') { $SkipVerify = [switch]$true }   # `iwr | iex` 传不了开关，用环境变量代替
if (-not $Version -and $env:PK_VERSION) { $Version = $env:PK_VERSION }
if ($env:PK_INSTALL_DIR) { $Dir = $env:PK_INSTALL_DIR }

# channel: explicit -Base > -Mirror cn (npm platform package) > gitee release
$from = if ($Base) { 'base' } elseif ($Mirror -eq 'cn') { 'npm' } else { 'gitee' }

# npm channel resolves the version from that package's own dist-tags (inside the branch below)
if (-not $Version -and $from -ne 'npm') { $Version = Get-LatestVersion }
if ($Version) { $Version = $Version.TrimStart('v') }   # allow -Version v0.7.0

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
    Download-File -url $dist.tarball -out $tgz

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
    & (Get-TarCommand) -xzf $tgz -C $extract
    if ($LASTEXITCODE -ne 0) { throw "Extraction failed (tar exit code $LASTEXITCODE)" }
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
    try {
      Download-File -url $url -out $zip
    } catch {
      # 国内直连 gitee 偶发被重置：自动退到 npmmirror 的 npm 平台包（另一个 host，通常更快）
      Write-Warning "gitee 下载失败，改用 npmmirror 的 npm 平台包重试：$($_.Exception.Message)"
      $Mirror = 'cn'
      $from = 'npm'
      $pkg = "$Scope/win32-$arch"
      $encoded = $pkg -replace '/', '%2F'
      $meta = Invoke-RestMethod -Uri "$($Registry.TrimEnd('/'))/$encoded" -TimeoutSec 30
      $ver = $meta.'dist-tags'.latest
      $dist = $meta.versions.$ver.dist
      if (-not $dist) { throw "npm 平台包里没有 $ver" }
      $tgz = Join-Path $tmp.FullName 'pkg.tgz'
      Download-File -url $dist.tarball -out $tgz
      if (-not $SkipVerify -and $dist.shasum) {
        $got = (Get-FileHash -Path $tgz -Algorithm SHA1).Hash
        if ($got -ne $dist.shasum.ToUpper()) { throw "SHA1 mismatch after fallback" }
        Write-Host '  verify: sha1 OK (registry dist.shasum, fallback)'
      }
      $extract = Join-Path $tmp.FullName 'npmpkg'
      New-Item -ItemType Directory -Path $extract -Force | Out-Null
      & (Get-TarCommand) -xzf $tgz -C $extract
      if ($LASTEXITCODE -ne 0) { throw "Extraction failed (tar exit code $LASTEXITCODE)" }
      $final = Join-Path $extract 'package\bin\packnest.exe'
      if (-not (Test-Path $final)) { throw 'Platform package does not contain package/bin/packnest.exe' }
      $Version = $ver
    }

    # 走到这里 $final 还有值，说明刚才 gitee 下载成功、没走 npm 兜底
    if (-not $final) {
    if (-not $SkipVerify) {
      $sums = try {
        $sumsTmp = Join-Path $tmp.FullName 'checksums.txt'
        Download-File -url $sumsUrl -out $sumsTmp -rounds 2
        Get-Content -Raw $sumsTmp
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
  }

  New-Item -ItemType Directory -Path $Dir -Force | Out-Null
  Copy-Item $final (Join-Path $Dir 'packnest.exe') -Force
  # pk 与 packnest 指向同一个二进制（Windows 没有可靠的 symlink，直接复制一份）
  Copy-Item (Join-Path $Dir 'packnest.exe') (Join-Path $Dir 'pk.exe') -Force

  # record how pk itself was installed (used by `pk uninstall --all --self`)
  try {
    $root = if ($env:PK_ROOT) { $env:PK_ROOT } else { Join-Path $HOME '.packnest' }
    $stateDir = Join-Path $root 'state'
    New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    $record = [ordered]@{ schema = 1; channel = 'script'; platform = "windows-$arch"; version = $Version; dir = $Dir }
    ($record | ConvertTo-Json -Compress) | Set-Content -Path (Join-Path $stateDir 'install.json') -Encoding UTF8
  } catch { }

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
