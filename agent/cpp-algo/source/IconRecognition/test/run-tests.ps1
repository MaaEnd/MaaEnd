[CmdletBinding()]
param(
    [ValidateSet("configure", "build", "quick", "manual")]
    [string]$Task,
    [Alias("h", "?")]
    [switch]$Help,
    [switch]$All,
    [ValidateSet("trade", "transfer", "port_storager", "valuables", "shipment", "credit_trade", "single_roi")]
    [string]$GridType,
    [string]$Image,
    [ValidateSet("full", "left", "right", "split", "all")]
    [string]$Side = "full",
    [ValidateRange(1, 64)]
    [int]$Jobs,
    [string]$CMakePath,
    [string]$VsDevShellPath,
    [string]$Configuration = "RelWithDebInfo"
)

$ErrorActionPreference = "Stop"
$testRoot = $PSScriptRoot
$repoRoot = (Resolve-Path -LiteralPath (Join-Path $testRoot "../../../../..")).Path
$buildRoot = Join-Path $testRoot "build"

function Show-Usage {
    @"
用法:
  ./run-tests.ps1 -Task configure
  ./run-tests.ps1 -Task build
  ./run-tests.ps1 -Task quick
  ./run-tests.ps1 -Task manual -All [-Side full|left|right|split|all] [-Jobs <1..64>] [-Debug]
  ./run-tests.ps1 -Task manual -GridType <type> [-Image <basename>] [-Side full|left|right|split|all] [-Jobs <1..64>] [-Debug]
  ./run-tests.ps1 -Task manual -Image <basename> [-Jobs <1..64>] [-Debug]
  ./run-tests.ps1 -Help|-h

网格类型:
  trade, transfer, port_storager, valuables, shipment, credit_trade, single_roi

Side 仅用于 transfer 和 port_storager；默认使用 full。
Jobs 的命令行参数优先于本机配置；未配置时使用 1。
"@
}

if ($Help -or $PSBoundParameters.Count -eq 0) {
    Show-Usage
    return
}

if (-not $Task) {
    Show-Usage
    throw "必须显式指定 -Task。"
}

$localConfigPath = Join-Path $testRoot "run-tests.local.psd1"
$localConfig = @{}
if (Test-Path -LiteralPath $localConfigPath -PathType Leaf) {
    $localConfig = Import-PowerShellDataFile -LiteralPath $localConfigPath
    $allowedKeys = @("CMakePath", "VsDevShellPath", "Jobs")
    $unknownKeys = @($localConfig.Keys | Where-Object { $_ -notin $allowedKeys })
    if ($unknownKeys.Count -gt 0) {
        throw "本地测试配置包含未知字段: $($unknownKeys -join ', ')"
    }
    foreach ($key in $localConfig.Keys) {
        if ($key -eq "Jobs") {
            if ($localConfig[$key] -isnot [int] -or $localConfig[$key] -lt 1 -or $localConfig[$key] -gt 64) {
                throw "本地测试配置 Jobs 必须是 1..64 的整数"
            }
            continue
        }
        if ($localConfig[$key] -isnot [string] -or [string]::IsNullOrWhiteSpace($localConfig[$key])) {
            throw "本地测试配置 $key 必须是非空字符串"
        }
    }
}

# 显式命令行参数优先，其次使用本机配置，最后回退到可移植默认值。
if (-not $PSBoundParameters.ContainsKey("CMakePath")) {
    $CMakePath = if ($localConfig.ContainsKey("CMakePath")) { $localConfig.CMakePath } else { "cmake" }
}
if (-not $PSBoundParameters.ContainsKey("VsDevShellPath")) {
    $VsDevShellPath = if ($localConfig.ContainsKey("VsDevShellPath")) { $localConfig.VsDevShellPath } else { "" }
}
if (-not $PSBoundParameters.ContainsKey("Jobs")) {
    $Jobs = if ($localConfig.ContainsKey("Jobs")) { $localConfig.Jobs } else { 1 }
}

if ([System.IO.Path]::IsPathRooted($CMakePath) -and -not (Test-Path -LiteralPath $CMakePath -PathType Leaf)) {
    throw "未找到 CMake: $CMakePath"
}
if ($VsDevShellPath -and -not (Test-Path -LiteralPath $VsDevShellPath -PathType Leaf)) {
    throw "未找到 Visual Studio Developer PowerShell: $VsDevShellPath"
}

if ($VsDevShellPath) {
    & $VsDevShellPath -Arch amd64 -HostArch amd64
}

function Invoke-CMake {
    param([string[]]$Arguments)
    & $CMakePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "CMake 执行失败，退出码: $LASTEXITCODE"
    }
}

function Ensure-Configured {
    if (-not (Test-Path -LiteralPath (Join-Path $buildRoot "CMakeCache.txt"))) {
        Invoke-CMake -Arguments @("-S", $testRoot, "-B", $buildRoot)
    }
}

function Build-Targets {
    param([string[]]$Targets)
    Ensure-Configured
    $arguments = @("--build", $buildRoot, "--config", $Configuration, "--target") + $Targets
    Invoke-CMake -Arguments $arguments
}

function Find-TestExecutable {
    param([string]$Name)
    $executable = Get-ChildItem -LiteralPath $buildRoot -Recurse -Filter "$Name.exe" |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if ($null -eq $executable) {
        throw "未找到测试程序: $Name.exe"
    }
    return $executable.FullName
}

function Set-TestRuntimePath {
    $runtimeDirectories = @(
        (Join-Path $repoRoot "deps/bin"),
        (Join-Path $repoRoot "agent/cpp-algo/MaaUtils/MaaDeps/vcpkg/installed/maa-x64-windows/bin"),
        (Join-Path $repoRoot "agent/cpp-algo/build/bin/RelWithDebInfo")
    ) | Where-Object { Test-Path -LiteralPath $_ }
    $env:PATH = ($runtimeDirectories -join ";") + ";" + $env:PATH
}

Set-Location -LiteralPath $repoRoot
switch ($Task) {
    "configure" {
        Invoke-CMake -Arguments @("-S", $testRoot, "-B", $buildRoot)
    }
    "build" {
        Build-Targets -Targets @(
            "icon-recognition-types-tests",
            "icon-recognition-manual-cli-tests",
            "icon-recognition-small-tests",
            "icon-recognition-custom-tests",
            "icon-recognition-debug-tests",
            "icon-recognition-manual-runner"
        )
    }
    "quick" {
        Build-Targets -Targets @(
            "icon-recognition-types-tests",
            "icon-recognition-manual-cli-tests",
            "icon-recognition-small-tests",
            "icon-recognition-custom-tests",
            "icon-recognition-debug-tests"
        )
        Set-TestRuntimePath
        foreach ($name in @(
            "icon-recognition-types-tests",
            "icon-recognition-manual-cli-tests",
            "icon-recognition-small-tests",
            "icon-recognition-custom-tests",
            "icon-recognition-debug-tests"
        )) {
            & (Find-TestExecutable -Name $name)
            if ($LASTEXITCODE -ne 0) {
                throw "$name 执行失败，退出码: $LASTEXITCODE"
            }
        }
    }
    "manual" {
        if ($All -and ($GridType -or $Image)) {
            Show-Usage
            throw "-All 不能与 -GridType 或 -Image 同时使用。"
        }
        if (-not $All -and -not $GridType -and -not $Image) {
            Show-Usage
            throw "manual 任务必须指定 -All、-GridType 或 -Image。"
        }
        Build-Targets -Targets @("icon-recognition-manual-runner")
        Set-TestRuntimePath
        $arguments = @()
        if ($All) {
            $arguments += "--all"
        }
        else {
            if ($GridType) {
                $arguments += @("--grid-type", $GridType)
            }
            if ($Image) {
                $arguments += @("--image", $Image)
            }
        }
        if ($PSBoundParameters.ContainsKey("Side")) {
            $arguments += @("--side", $Side)
        }
        $arguments += @("--jobs", $Jobs)
        if ($PSBoundParameters.ContainsKey("Debug")) {
            $arguments += "--debug"
        }
        & (Find-TestExecutable -Name "icon-recognition-manual-runner") @arguments
        if ($LASTEXITCODE -ne 0) {
            throw "icon-recognition-manual-runner 执行失败，退出码: $LASTEXITCODE"
        }
    }
}
