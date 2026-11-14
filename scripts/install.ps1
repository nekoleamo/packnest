<#
packnest installer for Windows (never touches GitHub)

  iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex

  -Mirror default|cn   default = gitee release zip (fast in China too, recommended)
                       cn      = 保留兼容（2026-11-11 起都走 gitee；npm 通道已停用）
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

# Get-Text 把任意响应体转成去空白的字符串。
#
# 为什么不能直接 `.Content.Trim()`：python 的 http.server 对没有扩展名的 `latest` 文件
# 返回 application/octet-stream，这时 Invoke-WebRequest 的 .Content 是 **byte[]**，
# 调 .Trim() 直接抛 MethodException，被 catch 吞掉之后就去问 gitee 了——
# 2026-11-15 实测：局部源 `-Base http://127.0.0.1:PORT` 反而拼出 gitee 上的最新版本号，
# 下载一个本地根本不存在的 vX 目录（404）。
function Get-Text {
  param([Parameter(Mandatory)][string]$url, [int]$timeout = 15)
  $resp = Invoke-WebRequest -Uri $url -TimeoutSec $timeout -UseBasicParsing
  $raw = $resp.Content
  if ($raw -is [byte[]]) { $raw = [Text.Encoding]::UTF8.GetString($raw) }
  return ([string]$raw).Trim()
}

function Get-LatestVersion {
  if ($Base) {
    # -Base 是权威源：本地/自托管源上查不到版本就直接失败，**绝不**回退到 gitee 的
    # 最新版本号（那会拼出一个该源上不存在的目录，表现为下载 404，极难看出原因）。
    try {
      $v = Get-Text -url "$($Base.TrimEnd('/'))/latest"
      if ($v) { return $v.TrimStart('v') }
    } catch { }
    throw "Cannot read $($Base.TrimEnd('/'))/latest; pass -Version explicitly (a -Base source is authoritative, gitee is never used as a fallback)."
  }
  try {
    $release = Invoke-RestMethod -Uri "https://gitee.com/api/v5/repos/$GiteeRepo/releases/latest" -TimeoutSec 15
    # releases/latest can point at the GUI's own tag (gui-vX.Y.Z), which has no CLI artifacts —
    # relying on it produced a "vgui-v0.10.4" download URL (404). Only accept plain vX.Y.Z.
    if ($release.tag_name -match '^v\d+\.\d+\.\d+$') { return $release.tag_name.TrimStart('v') }
  } catch { }
  # gitee's API occasionally answers 401 anonymously: fall back to the single source of truth
  # in the repo (scripts/latest, bumped by `make release` and checked by verify-release).
  try {
    $v = Get-Text -url "https://gitee.com/$GiteeRepo/raw/main/scripts/latest"
    if ($v) { return $v.TrimStart('v') }
  } catch { }
  throw 'Cannot detect the latest version; pass -Version explicitly.'
}

if ($env:PK_SKIP_VERIFY -eq '1') { $SkipVerify = [switch]$true }   # `iwr | iex` 传不了开关，用环境变量代替

# 发布签名公钥（allowed_signers 格式，与 pk 内置的那份同源）。
# 发版前把 scripts/sign-release.sh 打印的那行填进来；为空则只做 sha256 并打印提示。
$AllowedSigners = 'release@packnest.dev ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJEVfrMrDRIA2v/PHcPC6gzhU4q0cc+ak8KRr5idL/Xm pk-release-2026 pk-release-2026'
if (-not $Version -and $env:PK_VERSION) { $Version = $env:PK_VERSION }
if ($env:PK_INSTALL_DIR) { $Dir = $env:PK_INSTALL_DIR }

# channel: explicit -Base > gitee release（npm 平台包通道已于 2026-11-11 停用）
if ($Mirror -eq 'cn') { Write-Host 'note: -Mirror cn now also uses gitee (npm channel retired)' }
$from = if ($Base) { 'base' } else { 'gitee' }

if (-not $Version) { $Version = Get-LatestVersion }
if ($Version) { $Version = $Version.TrimStart('v') }   # allow -Version v0.7.0

$tmp = New-Item -ItemType Directory -Path (Join-Path $TempRoot ("pk-" + [guid]::NewGuid())) -Force
try {
  $final = $null   # path of the extracted packnest.exe we will install

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
    throw "Download failed: $url -- retry once, fix the network, or self-host with -Base. ($($_.Exception.Message))"
  }

  if (-not $SkipVerify) {
    $sums = try {
      $sumsTmp = Join-Path $tmp.FullName 'checksums.txt'
      Download-File -url $sumsUrl -out $sumsTmp -rounds 2
      Get-Content -Raw $sumsTmp
    } catch { $null }
    if ($null -eq $sums) { $sums = '' }

    # (1) 签名：远端有 .sig、脚本内置公钥、且本机有 ssh-keygen（Win10 1809+ 可选功能）才验
    $sigUrl = "$sumsUrl.sig"
    $sigTmp = Join-Path $tmp.FullName 'checksums.txt.sig'
    $haveSig = $false
    try { Download-File -url $sigUrl -out $sigTmp -rounds 1; $haveSig = $true } catch { $haveSig = $false }
    if ($haveSig) {
      $ssh = Get-Command ssh-keygen -ErrorAction SilentlyContinue
      if ($ssh -and $AllowedSigners) {
        $signersTmp = Join-Path $tmp.FullName 'allowed_signers'
        $sumsFile = Join-Path $tmp.FullName 'checksums.txt'
        Set-Content -Path $signersTmp -Value $AllowedSigners -NoNewline
        Set-Content -Path $sumsFile -Value (($sums.TrimEnd()) + "`n") -NoNewline
        # 待验数据必须走**文件重定向**，不能用 PowerShell 管道：管道会给最后一行再补一个换行，
        # ssh-keygen 看到的字节就跟签名时不一致了 → 永远验不过（Windows CI 首跑就是这么全红的）。
        $sigOut = Join-Path $tmp.FullName 'sig.out'
        $sigErr = Join-Path $tmp.FullName 'sig.err'
        $proc = Start-Process -FilePath $ssh.Source -NoNewWindow -Wait -PassThru `
          -ArgumentList @('-Y', 'verify', '-f', $signersTmp, '-I', 'release@packnest.dev',
            '-n', 'pk-release', '-s', $sigTmp) `
          -RedirectStandardInput $sumsFile -RedirectStandardOutput $sigOut -RedirectStandardError $sigErr
        if ($proc.ExitCode -ne 0) {
          $detail = ((Get-Content $sigErr -Raw -ErrorAction SilentlyContinue) +
            (Get-Content $sigOut -Raw -ErrorAction SilentlyContinue))
          if ($detail) { $detail = $detail.Trim() }
          throw "Signature verification failed for $sigUrl (artifact or checksums may be tampered with). $detail"
        }
        Write-Host '  verify: sshsig OK (release signature)'
      } else {
        Write-Host '  note: signature present but not verified (no ssh-keygen or no built-in key); sha256 only' -ForegroundColor Yellow
      }
    }

    $want = $null
    foreach ($line in ($sums -split "`n")) {
      $l = $line.Trim()
      if (-not $l) { continue }
      $f = $l -split '\s+'
      if ($f.Count -lt 2) { continue }
      # 有的 sha256sum 是二进制模式：文件名前多一个 `*`（sha256sum -b / Git Bash）
      $fn = $f[$f.Count - 1].TrimStart('*')
      if ($fn -eq "${name}.zip") { $want = $f[0]; break }
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
