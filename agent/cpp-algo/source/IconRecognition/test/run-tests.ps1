param(
    [ValidateSet("configure", "build", "quick", "manual")]
    [string]$Task,
    [Alias("h", "?")]
    [switch]$Help,
    [switch]$All,
    [switch]$Debug,
    [switch]$UseLocalExpected,
    [ValidateSet("trade", "transfer", "port_storager", "valuables", "shipment", "credit_trade", "rewards", "single_roi")]
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
$mergedInputRoot = Join-Path $buildRoot "merged-input"
$trackedInputRoot = Join-Path $repoRoot "tests/MaaEndTestset/Win32/Official_CN/IconRecognition"
$trackedExpectedPath = Join-Path $trackedInputRoot "expected.csv"
$localExpectedPath = Join-Path $testRoot "input/expected.csv"
$quickFixtures = @(
    "transfer/25.png",
    "transfer/57.png",
    "port_storager/1.png",
    "credit_trade/1.png",
    "rewards/135.png",
    "single_roi/1177-450-54/1.png"
)

function Show-Usage {
    @"
用法:
  ./run-tests.ps1 -Task configure
  ./run-tests.ps1 -Task build
  ./run-tests.ps1 -Task quick
  ./run-tests.ps1 -Task manual -All [-UseLocalExpected] [-Side full|left|right|split|all] [-Jobs <1..64>] [-Debug]
  ./run-tests.ps1 -Task manual -GridType <type> [-Image <basename>] [-UseLocalExpected] [-Side full|left|right|split|all] [-Jobs <1..64>] [-Debug]
  ./run-tests.ps1 -Task manual -Image <basename> [-UseLocalExpected] [-Jobs <1..64>] [-Debug]
  ./run-tests.ps1 -Help|-h

网格类型:
  trade, transfer, port_storager, valuables, shipment, credit_trade, rewards, single_roi

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

function Copy-InputTree {
    param(
        [Parameter(Mandatory)] [string]$SourceRoot,
        [Parameter(Mandatory)] [string]$DestinationRoot,
        [switch]$PreferLocalConflicts,
        [switch]$MarkAsLocal
    )
    if (-not (Test-Path -LiteralPath $SourceRoot -PathType Container)) {
        return
    }
    $overwritten = [System.Collections.Generic.List[string]]::new()
    foreach ($file in Get-ChildItem -LiteralPath $SourceRoot -Recurse -File) {
        $relative = $file.FullName.Substring($SourceRoot.Length).TrimStart('\', '/')
        # expected.csv 是独立校验基线，只能通过显式 --expected 参数选择，不能混入图片输入树。
        if ($relative -eq "expected.csv") {
            continue
        }
        $destination = Join-Path $DestinationRoot $relative
        $destinationDirectory = Split-Path -Parent $destination
        New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
        if ($MarkAsLocal) {
            $base = [System.IO.Path]::GetFileNameWithoutExtension($destination)
            $extension = [System.IO.Path]::GetExtension($destination)
            $directory = Split-Path -Parent $destination
            $suffix = 1
            do {
                $candidate = Join-Path $directory "$base.local$suffix$extension"
                $suffix++
            } while (Test-Path -LiteralPath $candidate -PathType Leaf)
            $destination = $candidate
        }
        elseif (Test-Path -LiteralPath $destination -PathType Leaf) {
            if ($PreferLocalConflicts) {
                $overwritten.Add($relative)
            }
            else {
                $base = [System.IO.Path]::GetFileNameWithoutExtension($destination)
                $extension = [System.IO.Path]::GetExtension($destination)
                $directory = Split-Path -Parent $destination
                $suffix = 1
                do {
                    $candidate = Join-Path $directory "$base.local$suffix$extension"
                    $suffix++
                } while (Test-Path -LiteralPath $candidate -PathType Leaf)
                $destination = $candidate
            }
        }
        Copy-Item -LiteralPath $file.FullName -Destination $destination -Force
    }
    if ($overwritten.Count -gt 0) {
        $examples = @($overwritten | Select-Object -First 5) -join ", "
        $suffix = if ($overwritten.Count -gt 5) { " 等" } else { "" }
        Write-Warning "本地测试素材覆盖了 $($overwritten.Count) 个子模块同名文件: $examples$suffix"
    }
}

function Prepare-MergedInput {
    param(
        [switch]$RequireQuickFixtures,
        [switch]$PreferLocalConflicts
    )
    if (-not (Test-Path -LiteralPath $trackedInputRoot -PathType Container)) {
        throw "缺少已跟踪的 IconRecognition 测试素材: $trackedInputRoot"
    }
    Remove-Item -LiteralPath $mergedInputRoot -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $mergedInputRoot -Force | Out-Null
    Copy-InputTree -SourceRoot $trackedInputRoot -DestinationRoot $mergedInputRoot
    Copy-InputTree `
        -SourceRoot (Join-Path $testRoot "input") `
        -DestinationRoot $mergedInputRoot `
        -PreferLocalConflicts:$PreferLocalConflicts `
        -MarkAsLocal:$(-not $PreferLocalConflicts)
    if ($RequireQuickFixtures) {
        $missing = @($quickFixtures | Where-Object { -not (Test-Path -LiteralPath (Join-Path $trackedInputRoot $_) -PathType Leaf) })
        if ($missing.Count -gt 0) {
            throw "子模块中的 quick 测试素材缺失: $($missing -join ', ')"
        }
    }
}

function Resolve-ExpectedResultsPath {
    param([switch]$UseLocal)
    if ($UseLocal) {
        if (Test-Path -LiteralPath $localExpectedPath -PathType Leaf) {
            return $localExpectedPath
        }
        throw "显式请求了本地 expected.csv，但文件不存在: $localExpectedPath"
    }
    if (Test-Path -LiteralPath $trackedExpectedPath -PathType Leaf) {
        return $trackedExpectedPath
    }
    throw "缺少 IconRecognition expected 结果: $trackedExpectedPath"
}

function Test-LocalImageSelection {
    param(
        [Parameter(Mandatory)] [string]$ImageName,
        [string]$SelectedGridType
    )
    $localRoot = Join-Path $testRoot "input"
    if ($SelectedGridType) {
        $localRoot = Join-Path $localRoot $SelectedGridType
    }
    if (-not (Test-Path -LiteralPath $localRoot -PathType Container)) {
        return $false
    }
    return $null -ne (Get-ChildItem -LiteralPath $localRoot -Recurse -File | Where-Object { $_.Name -eq $ImageName } | Select-Object -First 1)
}

function Invoke-QuickImageFixture {
    param(
        [Parameter(Mandatory)] [string]$Fixture,
        [Parameter(Mandatory)] [string]$ExpectedPath
    )
    $parts = $Fixture -split '/'
    $gridType = $parts[0]
    $image = $parts[-1]
    & (Find-TestExecutable -Name "icon-recognition-manual-runner") --grid-type $gridType --image $image --jobs 1 --expected $ExpectedPath
    if ($LASTEXITCODE -ne 0) {
        throw "quick 图片回归失败: $Fixture，退出码: $LASTEXITCODE"
    }
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
            "icon-recognition-expected-tests",
            "icon-recognition-manual-runner"
        )
    }
    "quick" {
        Prepare-MergedInput -RequireQuickFixtures
        $expectedPath = Resolve-ExpectedResultsPath -UseLocal:$UseLocalExpected
        Build-Targets -Targets @(
            "icon-recognition-types-tests",
            "icon-recognition-manual-cli-tests",
            "icon-recognition-small-tests",
            "icon-recognition-custom-tests",
            "icon-recognition-debug-tests",
            "icon-recognition-expected-tests",
            "icon-recognition-manual-runner"
        )
        Set-TestRuntimePath
        foreach ($name in @(
            "icon-recognition-types-tests",
            "icon-recognition-manual-cli-tests",
            "icon-recognition-small-tests",
            "icon-recognition-custom-tests",
            "icon-recognition-debug-tests",
            "icon-recognition-expected-tests"
        )) {
            & (Find-TestExecutable -Name $name)
            if ($LASTEXITCODE -ne 0) {
                throw "$name 执行失败，退出码: $LASTEXITCODE"
            }
        }
        foreach ($fixture in $quickFixtures) {
            Invoke-QuickImageFixture -Fixture $fixture -ExpectedPath $expectedPath
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
        $usesLocalImage = $PSBoundParameters.ContainsKey("Image") -and (Test-LocalImageSelection -ImageName $Image -SelectedGridType $GridType)
        Prepare-MergedInput -PreferLocalConflicts:($usesLocalImage -or $UseLocalExpected)
        if ($usesLocalImage) {
            Write-Warning "显式 -Image 命中本地 input 素材，本次优先使用本地同名图片并仅作人工审计: $Image"
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
        if (($UseLocalExpected -or -not $usesLocalImage) -and $Side -eq "full") {
            $arguments += @("--expected", (Resolve-ExpectedResultsPath -UseLocal:$UseLocalExpected))
        }
        elseif (-not $usesLocalImage) {
            Write-Warning "expected.csv 仅维护 full 基线，显式分侧运行仅作人工审计: $Side"
        }
        & (Find-TestExecutable -Name "icon-recognition-manual-runner") @arguments
        if ($LASTEXITCODE -ne 0) {
            throw "icon-recognition-manual-runner 执行失败，退出码: $LASTEXITCODE"
        }
    }
}
