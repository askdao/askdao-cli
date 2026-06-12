# [INPUT]: 依赖 GitHub Releases API (releases/latest) 与 GoReleaser 资产命名
#          askdao_{ver}_windows_{arch}.zip + checksums.txt；可选 $env:ASKDAO_VERSION 钉版本
# [OUTPUT]: 安装 askdao.exe 到 %LOCALAPPDATA%\askdao\bin 并幂等追加用户 PATH
# [POS]: install/ 的 Windows 安装器（PowerShell 5.1 兼容），被 https://askdao.ai/install.ps1
#        反向代理分发；与 install.sh（Unix）逻辑对齐；install.cmd 是其 CMD 包装
# [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
#
# Usage:  irm https://askdao.ai/install.ps1 | iex
# Pin:    $env:ASKDAO_VERSION = "0.1.0"; irm https://askdao.ai/install.ps1 | iex
$ErrorActionPreference = 'Stop'

$Repo = 'askdao/askdao-cli'
$InstallDir = Join-Path $env:LOCALAPPDATA 'askdao\bin'

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$version = $env:ASKDAO_VERSION
if (-not $version) {
  $latest = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
  $version = $latest.tag_name
}
$version = $version.TrimStart('v')

$asset = "askdao_${version}_windows_${arch}.zip"
$base = "https://github.com/$Repo/releases/download/v$version"

$tmp = Join-Path ([IO.Path]::GetTempPath()) "askdao-install-$([IO.Path]::GetRandomFileName())"
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Write-Host "Downloading askdao v$version (windows/$arch) ..." -ForegroundColor Green
  Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
  Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

  $expected = (Select-String -Path (Join-Path $tmp 'checksums.txt') -Pattern ([regex]::Escape($asset))).Line.Split(' ')[0]
  $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
  if ($expected -ne $actual) { throw "checksum mismatch for $asset (expected $expected, got $actual)" }

  Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath $tmp -Force
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  $exe = Join-Path $InstallDir 'askdao.exe'
  # 升级场景：运行中的旧 exe 不能覆盖，先挪走（askdao update 同款策略）
  if (Test-Path $exe) {
    Remove-Item "$exe.old" -ErrorAction SilentlyContinue
    Move-Item $exe "$exe.old" -Force
  }
  Move-Item (Join-Path $tmp 'askdao.exe') $exe -Force
  Remove-Item "$exe.old" -ErrorAction SilentlyContinue

  # 幂等追加用户 PATH（只读用户级，避免把机器级 PATH 写进用户级）
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (($userPath -split ';') -notcontains $InstallDir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$InstallDir", 'User')
  }
  # 当前会话立即可用
  if (($env:Path -split ';') -notcontains $InstallDir) {
    $env:Path = "$env:Path;$InstallDir"
  }

  Write-Host "Installed: $exe ($(& $exe version))" -ForegroundColor Green
  Write-Host 'Note: other open terminals need to be restarted to pick up PATH.'
  Write-Host 'Next: askdao auth login' -ForegroundColor Green
}
finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
