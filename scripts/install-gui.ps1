# packnest desktop (GUI) installer for Windows -- the CLI must be installed first.
#
#   iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install-gui.ps1 -UseB | iex
#
# Options:
#   -Base URL          any static host base pointing at a gui-v<version> folder (highest priority)
#   -Version vX.Y.Z    GUI version (default: newest gui-v* release)
#   -Dir DIR           install dir (default: $env:LOCALAPPDATA\packnest\gui)
#   -Repo owner/name   gitee repo (default null_593_5354/packnest)
#   -SkipVerify        skip sha256 + signature check (not recommended)
#   -DryRun            print the plan only
#
# Verification equals the CLI installer: verify the sshsig of checksums.txt first, then the
# artifact sha256. The GUI binary is unsigned (no code-signing cert), so SmartScreen may warn
# on first launch -- this script says so instead of pretending everything is fine.
#
# NOTE: every user-facing message is ASCII on purpose (`iwr | iex` decodes with the console
# codepage; a UTF-8 body turns Chinese literals into mojibake). Comments stay Chinese.
param(
  [string]$Base = $env:PK_GUI_BASE,
  [string]$Version = '',
  [string]$Dir = '',
  [string]$Repo = '',
  [switch]$SkipVerify,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$GiteeRepo = if ($Repo) { $Repo } elseif ($env:PK_GITEE_REPO) { $env:PK_GITEE_REPO } else { 'null_593_5354/packnest' }
if (-not $Dir) {
  $Dir = if ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'packnest\gui' } else { Join-Path $HOME '.packnest\gui' }
}
if ($env:PK_SKIP_VERIFY -eq '1') { $SkipVerify = [switch]$true }

# 发布签名公钥（与 pk 内置、install.sh、install-gui.sh 同源；`make keys` 核对）
$AllowedSigners = 'release@packnest.dev ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJEVfrMrDRIA2v/PHcPC6gzhU4q0cc+ak8KRr5idL/Xm pk-release-2026 pk-release-2026'

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'ARM64' { 'arm64' }
  'AMD64' { 'amd64' }
  default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

# 先装 CLI：GUI 只是 pk 的可视化壳
$pk = Get-Command pk -ErrorAction SilentlyContinue
if (-not $pk) {
  throw 'Install the CLI first: iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex'
}

function Get-GuiVersion {
  if ($Base) {
    try {
      $v = (Invoke-WebRequest -Uri "$($Base.TrimEnd('/'))/latest" -TimeoutSec 15 -UseBasicParsing).Content.Trim()
      if ($v) { return $v.TrimStart('v') }
    } catch { }
  }
  # gitee 的 releases/latest 指向 CLI 的 v*，GUI 是 gui-v*，只能自己挑最大的
  try {
    $list = Invoke-RestMethod -Uri "https://gitee.com/api/v5/repos/$GiteeRepo/releases?per_page=50&page=1" -TimeoutSec 20
    $tags = @($list | ForEach-Object { $_.tag_name } | Where-Object { $_ -match '^gui-v\d+\.\d+\.\d+$' })
    if ($tags.Count -gt 0) {
      $best = $tags | Sort-Object { [version]($_ -replace '^gui-v', '') } | Select-Object -Last 1
      return ($best -replace '^gui-v', '')
    }
  } catch { }
  throw 'Cannot detect the GUI version; pass -Version explicitly.'
}

function Download-File {
  param([string]$url, [string]$out, [int]$rounds = 3)
  $last = $null
  for ($i = 1; $i -le $rounds; $i++) {
    foreach ($mode in @('webclient-noproxy', 'iwr')) {
      try {
        if (Test-Path $out) { Remove-Item $out -Force -ErrorAction SilentlyContinue }
        if ($mode -eq 'webclient-noproxy') {
          $wc = New-Object System.Net.WebClient
          $wc.Proxy = $null
          $wc.Headers.Add('User-Agent', 'packnest-gui-installer')
          $wc.DownloadFile($url, $out)
        } else {
          Invoke-WebRequest -Uri $url -OutFile $out -UseBasicParsing -TimeoutSec 600
        }
        if ((Test-Path $out) -and ((Get-Item $out).Length -gt 0)) { return }
      } catch { $last = $_ }
    }
    Start-Sleep -Seconds $i
  }
  throw "Download failed: $url ($($last.Exception.Message))"
}

$ver = $Version.TrimStart('v')
if (-not $ver) { $ver = Get-GuiVersion }
if ($ver) { $ver = $ver.TrimStart('v') }
if (-not $ver -and $env:PK_VERSION) { $ver = $env:PK_VERSION.TrimStart('v') }

$name = "packnest-desktop_${ver}_windows_${arch}"
$file = "${name}.zip"
$baseUrl = if ($Base) {
  "$($Base.TrimEnd('/'))"
} else {
  "https://gitee.com/$GiteeRepo/releases/download/gui-v${ver}"
}
$url = "$baseUrl/$file"
$sumsUrl = "$baseUrl/checksums.txt"

Write-Host "Installing packnest desktop $ver (windows-$arch)"
Write-Host "  source: $url"
Write-Host "  target: $Dir"
if ($DryRun) { return }

$tmp = New-Item -ItemType Directory -Path (Join-Path ([IO.Path]::GetTempPath()) ("pk-gui-" + [guid]::NewGuid())) -Force
try {
  $zip = Join-Path $tmp.FullName 'gui.zip'
  Download-File -url $url -out $zip

  if (-not $SkipVerify) {
    $sums = try {
      $sumsTmp = Join-Path $tmp.FullName 'checksums.txt'
      Download-File -url $sumsUrl -out $sumsTmp -rounds 2
      Get-Content -Raw $sumsTmp
    } catch { '' }
    if (-not $sums) { throw "Cannot fetch $sumsUrl; re-run with -SkipVerify only if you trust the source." }

    $sigTmp = Join-Path $tmp.FullName 'checksums.txt.sig'
    $haveSig = $false
    try { Download-File -url "$sumsUrl.sig" -out $sigTmp -rounds 1; $haveSig = $true } catch { $haveSig = $false }
    if ($haveSig) {
      $ssh = Get-Command ssh-keygen -ErrorAction SilentlyContinue
      if ($ssh -and $AllowedSigners) {
        $signersTmp = Join-Path $tmp.FullName 'allowed_signers'
        $sumsFile = Join-Path $tmp.FullName 'checksums.txt'
        Set-Content -Path $signersTmp -Value $AllowedSigners -NoNewline
        Set-Content -Path $sumsFile -Value $sums -NoNewline
        $verifyOut = (Get-Content $sumsFile -Raw | & ssh-keygen -Y verify -f $signersTmp `
          -I 'release@packnest.dev' -n pk-release -s $sigTmp 2>&1) -join "`n"
        if ($verifyOut -notmatch 'Good "pk-release" signature') {
          throw "Signature verification failed for $sumsUrl.sig (manifest or artifact may be tampered with)."
        }
        Write-Host '  verify: sshsig OK (release signature)'
      } else {
        Write-Host '  note: signature present but not verified (no ssh-keygen or no built-in key); sha256 only' -ForegroundColor Yellow
      }
    }

    $want = $null
    foreach ($line in ($sums -split "`n")) {
      $f = $line.Trim() -split '\s+'
      if ($f.Count -ge 2 -and $f[1] -eq $file) { $want = $f[0]; break }
    }
    if (-not $want) { throw "No sha256 for $file in $sumsUrl. Re-run with -SkipVerify only if you trust the source." }
    $got = (Get-FileHash -Path $zip -Algorithm SHA256).Hash
    if ($got -ne $want.ToUpper()) {
      throw "SHA256 mismatch (expected $want, got $got). The artifact may be tampered with or truncated."
    }
    Write-Host '  verify: sha256 OK (checksums.txt)'
  }

  $extract = Join-Path $tmp.FullName 'unz'
  New-Item -ItemType Directory -Path $extract -Force | Out-Null
  Expand-Archive -Path $zip -DestinationPath $extract -Force
  $exe = Join-Path $extract 'pk-desktop.exe'
  if (-not (Test-Path $exe)) { throw 'The zip does not contain pk-desktop.exe' }
  New-Item -ItemType Directory -Path $Dir -Force | Out-Null
  Copy-Item $exe (Join-Path $Dir 'pk-desktop.exe') -Force

  # 开始菜单快捷方式（GUI 不进 PATH：它不是命令行工具）
  try {
    $lnk = Join-Path ([Environment]::GetFolderPath('Programs')) 'packnest.lnk'
    $sh = New-Object -ComObject WScript.Shell
    $s = $sh.CreateShortcut($lnk)
    $s.TargetPath = (Join-Path $Dir 'pk-desktop.exe')
    $s.WorkingDirectory = $Dir
    $s.Description = 'packnest desktop'
    $s.Save()
    Write-Host ''
  } catch { }   # 建不出快捷方式不算失败

  Write-Host "Installed: $(Join-Path $Dir 'pk-desktop.exe')"
  Write-Host 'First launch: SmartScreen may warn about an unknown publisher (unsigned build).'
  Write-Host 'Next: open packnest from the Start Menu (it needs the pk CLI on PATH).'
} finally {
  Remove-Item $tmp.FullName -Recurse -Force -ErrorAction SilentlyContinue
}
