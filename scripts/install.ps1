<#
packnest 安装脚本（Windows，不访问 GitHub）

  iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex

  -Mirror default|cn   产物来源
                         default = gitee release 的 zip（国内也快，推荐）
                         cn      = 从 npmmirror 取 npm 平台包（@packnest/win32-<arch>）
  -Base URL            任意静态源基地址（自己托管的 OSS/CDN/内网），优先级最高
  -Version vX.Y.Z      指定版本
  -Dir DIR             安装目录（默认 $env:LOCALAPPDATA\packnest\bin）
  -SkipVerify          跳过哈希校验（不推荐；只在拿不到校验和时用）
  -DryRun              只打印
#>
param(
  [ValidateSet('default', 'cn')][string]$Mirror = 'default',
  [string]$Base = $env:PK_INSTALL_BASE,
  [string]$Version = '',
  [string]$Dir = (Join-Path $env:LOCALAPPDATA 'packnest\bin'),
  [switch]$SkipVerify,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # 大文件下载别刷进度条，慢且吵

$GiteeRepo = if ($env:PK_GITEE_REPO) { $env:PK_GITEE_REPO } else { 'null_593_5354/packnest' }
$Registry  = if ($env:PK_NPM_REGISTRY) { $env:PK_NPM_REGISTRY } else { 'https://registry.npmmirror.com' }
$Scope     = if ($env:PK_NPM_SCOPE) { $env:PK_NPM_SCOPE } else { '@packnest' }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'ARM64' { 'arm64' }
  'AMD64' { 'amd64' }
  default { throw "不支持的架构: $env:PROCESSOR_ARCHITECTURE" }
}

function Get-LatestVersion {
  if ($Base) {
    $v = (Invoke-WebRequest -Uri "$($Base.TrimEnd('/'))/latest" -TimeoutSec 15 -UseBasicParsing).Content.Trim()
    if ($v) { return $v.TrimStart('v') }
  }
  # npm registry 是公开的、不需要凭据（gitee 的 API 现在匿名 401，只能当最后兜底）
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
  throw '拿不到最新版本，请用 -Version 指定'
}

if (-not $Version) { $Version = Get-LatestVersion }
if (-not $Version) { throw '拿不到最新版本，请用 -Version 指定' }
$Version = $Version.TrimStart('v')   # 允许 -Version v0.7.0

# 通道决定：显式 -Base > -Mirror cn（npm 平台包）> gitee release
$from = if ($Base) { 'base' } elseif ($Mirror -eq 'cn') { 'npm' } else { 'gitee' }

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP ("pk-" + [guid]::NewGuid())) -Force
try {
  $exe = Join-Path $tmp.FullName 'packnest.exe'

  if ($from -eq 'npm') {
    $pkg = "$Scope/win32-$arch"
    $encoded = $pkg -replace '/', '%2F'
    $meta = Invoke-RestMethod -Uri "$($Registry.TrimEnd('/'))/$encoded" -TimeoutSec 30
    $ver = if ($Version) { $Version } else { $meta.'dist-tags'.latest }
    $dist = $meta.versions.$ver.dist
    if (-not $dist) { throw "$pkg 里没有 $ver 这个版本" }
    Write-Host "将要安装 packnest $ver（windows-$arch，npm 平台包 $pkg）"
    Write-Host "  来源: $($dist.tarball)"
    Write-Host "  目录: $Dir"
    if ($DryRun) { return }

    $tgz = Join-Path $tmp.FullName 'pkg.tgz'
    Invoke-WebRequest -Uri $dist.tarball -OutFile $tgz -TimeoutSec 300

    if (-not $SkipVerify) {
      $want = $dist.shasum
      if ($want) {
        $got = (Get-FileHash -Path $tgz -Algorithm SHA1).Hash
        if ($got -ne $want.ToUpper()) {
          throw "SHA1 校验失败（registry 声明 $want，实际 $got）；确认来源可信后可加 -SkipVerify"
        }
        Write-Host '  校验：sha1 OK（registry dist.shasum）'
      } else {
        Write-Warning '拿不到 registry 的 shasum，跳过校验（不推荐）'
      }
    }

    # Windows 10 1803+ 自带 bsdtar，够用（不引第三方解压）
    $extract = Join-Path $tmp.FullName 'x'
    New-Item -ItemType Directory -Path $extract -Force | Out-Null
    & tar.exe -xzf $tgz -C $extract
    if ($LASTEXITCODE -ne 0) { throw "解压失败（tar.exe 退出码 $LASTEXITCODE）" }
    $src = Join-Path $extract 'package/bin/packnest.exe'
    if (-not (Test-Path $src)) { throw "平台包里没有 package/bin/packnest.exe" }
    Copy-Item $src $exe -Force
    $sumsUrl = $null
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
    Write-Host "将要安装 packnest $Version（windows-$arch）"
    Write-Host "  来源: $url"
    Write-Host "  目录: $Dir"
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
        throw "拿不到 ${name}.zip 的 sha256（$sumsUrl）；确认来源可信后可加 -SkipVerify"
      }
      $got = (Get-FileHash -Path $zip -Algorithm SHA256).Hash
      if ($got -ne $want.ToUpper()) {
        throw "SHA256 校验失败：期望 $want，实际 $got（产物可能被篡改或损坏）"
      }
      Write-Host '  校验：sha256 OK（checksums.txt）'
    }

    Expand-Archive -Path $zip -DestinationPath $tmp.FullName -Force
    $src = Join-Path $tmp.FullName 'packnest.exe'
    if (-not (Test-Path $src)) { throw "zip 里没有 packnest.exe" }
    Copy-Item $src $exe -Force
  }

  New-Item -ItemType Directory -Path $Dir -Force | Out-Null
  Copy-Item $exe (Join-Path $Dir 'packnest.exe') -Force
  # pk 与 packnest 指向同一个二进制（复制一份，Windows 没有 symlink 保证）
  Copy-Item (Join-Path $Dir 'packnest.exe') (Join-Path $Dir 'pk.exe') -Force

  Write-Host "装好了：$(Join-Path $Dir 'pk.exe')"
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($userPath -notlike "*$Dir*") {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$Dir", 'User')
    Write-Host "已把 $Dir 加进用户 PATH（新开终端生效）"
  }
  Write-Host '下一步：pk install node@22'
} finally {
  Remove-Item $tmp.FullName -Recurse -Force -ErrorAction SilentlyContinue
}
