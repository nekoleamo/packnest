<#
packnest 安装脚本（Windows，不访问 GitHub）

  iwr https://gitee.com/packnest/packnest/raw/master/scripts/install.ps1 -UseB | iex

  -Mirror cn|default   产物来源（cn 从 npmmirror 取）
  -Version vX.Y.Z      指定版本
  -Dir DIR             安装目录（默认 $env:LOCALAPPDATA\packnest\bin）
  -DryRun              只打印
#>
param(
  [ValidateSet('cn', 'default')][string]$Mirror = 'default',
  [string]$Version = '',
  [string]$Dir = (Join-Path $env:LOCALAPPDATA 'packnest\bin'),
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'ARM64' { 'arm64' }
  'AMD64' { 'amd64' }
  default { throw "不支持的架构: $env:PROCESSOR_ARCHITECTURE" }
}

if (-not $Version) {
  $release = Invoke-RestMethod -Uri 'https://gitee.com/api/v5/repos/packnest/packnest/releases/latest' -TimeoutSec 15
  $Version = $release.tag_name.TrimStart('v')
}
if (-not $Version) { throw '拿不到最新版本，请用 -Version 指定' }

$name = "packnest_${Version}_windows_${arch}"
$url = if ($Mirror -eq 'cn') {
  "https://registry.npmmirror.com/-/binary/packnest/v${Version}/${name}.zip"
} else {
  "https://gitee.com/packnest/packnest/releases/download/v${Version}/${name}.zip"
}

Write-Host "将要安装 packnest $Version（windows-$arch）"
Write-Host "  来源: $url"
Write-Host "  目录: $Dir"
if ($DryRun) { return }

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP ("pk-" + [guid]::NewGuid())) -Force
try {
  $zip = Join-Path $tmp.FullName 'pk.zip'
  Invoke-WebRequest -Uri $url -OutFile $zip -TimeoutSec 300
  Expand-Archive -Path $zip -DestinationPath $tmp.FullName -Force
  New-Item -ItemType Directory -Path $Dir -Force | Out-Null
  Copy-Item (Join-Path $tmp.FullName 'packnest.exe') (Join-Path $Dir 'packnest.exe') -Force
  # pk 与 packnest 指向同一个二进制（复制一份，Windows 没有 symlink 保证）
  Copy-Item (Join-Path $Dir 'packnest.exe') (Join-Path $Dir 'pk.exe') -Force
  Write-Host "装好了：$(Join-Path $Dir 'pk.exe')"
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($userPath -notlike "*$Dir*") {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$Dir", 'User')
    Write-Host "已把 $Dir 加进用户 PATH（新开终端生效）"
  }
} finally {
  Remove-Item $tmp.FullName -Recurse -Force -ErrorAction SilentlyContinue
}
