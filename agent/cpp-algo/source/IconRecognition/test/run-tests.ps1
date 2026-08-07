[CmdletBinding()]
param(
    [ValidateSet("configure", "build", "quick")]
    [string]$Task = "quick",
    [string]$CMakePath = "cmake",
    [string]$VsDevShellPath = "",
    [string]$Configuration = "RelWithDebInfo"
)

$ErrorActionPreference = "Stop"
$testRoot = $PSScriptRoot
$repoRoot = (Resolve-Path -LiteralPath (Join-Path $testRoot "../../../../..")).Path
$buildRoot = Join-Path $testRoot "build"

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
            "icon-recognition-small-tests",
            "icon-recognition-custom-tests",
            "icon-recognition-debug-tests",
            "icon-recognition-manual-runner"
        )
    }
    "quick" {
        Build-Targets -Targets @(
            "icon-recognition-types-tests",
            "icon-recognition-small-tests",
            "icon-recognition-custom-tests",
            "icon-recognition-debug-tests"
        )
        Set-TestRuntimePath
        foreach ($name in @(
            "icon-recognition-types-tests",
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
}
