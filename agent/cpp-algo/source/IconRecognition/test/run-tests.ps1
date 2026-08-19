param(
    [ValidateSet("configure", "build", "quick", "manual")]
    [string]$Task,
    [Alias("h", "?")]
    [switch]$Help,
    [switch]$All,
    [switch]$Debug,
    [switch]$UseLocalExpected,
    [ValidateSet("win32", "adb")]
    [string]$Dataset,
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
$cppAlgoRoot = Join-Path $repoRoot "agent/cpp-algo"
$buildRoot = Join-Path $cppAlgoRoot "build"
$testBuildRoot = Join-Path $buildRoot "source/IconRecognition/test"
$mergedInputRoot = Join-Path $testBuildRoot "merged-input"
$datasetManifestPath = Join-Path $testRoot "dataset-manifest.psd1"
$localExpectedPath = Join-Path $testRoot "input/expected.csv"
$gridTypes = @(
    "trade",
    "transfer",
    "port_storager",
    "valuables",
    "shipment",
    "credit_trade",
    "rewards",
    "single_roi"
)

if (-not (Test-Path -LiteralPath $datasetManifestPath -PathType Leaf)) {
    throw "缺少 IconRecognition 数据集清单: $datasetManifestPath"
}
$datasetManifest = Import-PowerShellDataFile -LiteralPath $datasetManifestPath

function Show-Usage {
    @"
用法:
  ./run-tests.ps1 -Task configure
  ./run-tests.ps1 -Task build
  ./run-tests.ps1 -Task quick
  ./run-tests.ps1 -Task manual -All -Dataset <win32|adb> [-UseLocalExpected] [-Side full|left|right|split|all] [-Jobs <1..64>] [-Debug]
  ./run-tests.ps1 -Task manual -GridType <type> -Dataset <win32|adb> [-Image <basename>] [-UseLocalExpected] [-Side full|left|right|split|all] [-Jobs <1..64>] [-Debug]
  ./run-tests.ps1 -Task manual -Image <basename> -Dataset <win32|adb> [-UseLocalExpected] [-Jobs <1..64>] [-Debug]
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
    Invoke-CMake -Arguments @(
        "-S",
        $cppAlgoRoot,
        "-B",
        $buildRoot,
        "-DMAAEND_BUILD_ICON_RECOGNITION_TESTS=ON"
    )
}

function Build-Targets {
    param([string[]]$Targets)
    Ensure-Configured
    $arguments = @("--build", $buildRoot, "--config", $Configuration, "--parallel", $Jobs, "--target") + $Targets
    Invoke-CMake -Arguments $arguments
}

function Copy-InputTree {
    param(
        [Parameter(Mandatory)] [string]$SourceRoot,
        [Parameter(Mandatory)] [string]$DestinationRoot
    )
    if (-not (Test-Path -LiteralPath $SourceRoot -PathType Container)) {
        return
    }
    foreach ($file in Get-ChildItem -LiteralPath $SourceRoot -Recurse -File) {
        $relative = $file.FullName.Substring($SourceRoot.Length).TrimStart('\', '/')
        # expected.csv 是独立校验基线，只能通过显式 --expected 参数选择，不能混入图片输入树。
        if ($relative -eq "expected.csv") {
            continue
        }
        $destination = Join-Path $DestinationRoot $relative
        $destinationDirectory = Split-Path -Parent $destination
        New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
        Copy-Item -LiteralPath $file.FullName -Destination $destination -Force
    }
}

function Resolve-DatasetPaths {
    param([Parameter(Mandatory)] [ValidateSet("win32", "adb")] [string]$Name)
    if (-not $datasetManifest.ContainsKey($Name)) {
        throw "数据集清单中缺少 IconRecognition 数据集: $Name"
    }
    $config = $datasetManifest[$Name]
    $root = Join-Path $repoRoot $config.Root
    return @{
        Name          = $Name
        Root          = $root
        ExpectedPath  = Join-Path $root "expected.csv"
        RoisPath      = Join-Path $root "rois.json"
        QuickFixtures = $config.QuickFixtures
    }
}

function Prepare-DatasetInput {
    param([Parameter(Mandatory)] [ValidateSet("win32", "adb")] [string]$Name)
    $paths = Resolve-DatasetPaths -Name $Name
    foreach ($path in @($paths.Root, $paths.ExpectedPath, $paths.RoisPath)) {
        if (-not (Test-Path -LiteralPath $path)) {
            throw "缺少 ${Name} IconRecognition 数据集资源: $path"
        }
    }
    Remove-Item -LiteralPath $mergedInputRoot -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $mergedInputRoot -Force | Out-Null
    Copy-InputTree -SourceRoot $paths.Root -DestinationRoot $mergedInputRoot
    return $paths
}

function Prepare-QuickDatasetInput {
    param([Parameter(Mandatory)] [ValidateSet("win32", "adb")] [string]$Name)
    $paths = Resolve-DatasetPaths -Name $Name
    foreach ($path in @($paths.Root, $paths.ExpectedPath, $paths.RoisPath)) {
        if (-not (Test-Path -LiteralPath $path)) {
            throw "缺少 ${Name} IconRecognition 数据集资源: $path"
        }
    }

    Remove-Item -LiteralPath $mergedInputRoot -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $mergedInputRoot -Force | Out-Null
    foreach ($gridType in $gridTypes) {
        foreach ($fixture in @($paths.QuickFixtures[$gridType])) {
            $source = Join-Path $paths.Root $fixture
            if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
                throw "quick 图片不存在: ${Name}/$fixture"
            }
            $destination = Join-Path $mergedInputRoot $fixture
            New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
            Copy-Item -LiteralPath $source -Destination $destination -Force

            # 部分截图使用同名 JSON 覆盖默认 ROI 或参数，quick 子集必须一并保留。
            $sidecarSource = [System.IO.Path]::ChangeExtension($source, ".json")
            if (Test-Path -LiteralPath $sidecarSource -PathType Leaf) {
                Copy-Item -LiteralPath $sidecarSource -Destination ([System.IO.Path]::ChangeExtension($destination, ".json")) -Force
            }
        }
    }
    return $paths
}

function Resolve-ExpectedResultsPath {
    param(
        [Parameter(Mandatory)] [hashtable]$DatasetPaths,
        [switch]$UseLocal
    )
    if ($UseLocal) {
        if (Test-Path -LiteralPath $localExpectedPath -PathType Leaf) {
            return $localExpectedPath
        }
        throw "显式请求了本地 expected.csv，但文件不存在: $localExpectedPath"
    }
    if (Test-Path -LiteralPath $DatasetPaths.ExpectedPath -PathType Leaf) {
        return $DatasetPaths.ExpectedPath
    }
    throw "缺少 IconRecognition expected 结果: $($DatasetPaths.ExpectedPath)"
}

function Invoke-QuickDataset {
    param([Parameter(Mandatory)] [hashtable]$DatasetPaths)
    & (Find-TestExecutable -Name "icon-recognition-manual-runner") `
        --all `
        --jobs $Jobs `
        --dataset $DatasetPaths.Name `
        --expected $DatasetPaths.ExpectedPath `
        --rois $DatasetPaths.RoisPath
    if ($LASTEXITCODE -ne 0) {
        throw "quick 数据集回归失败: $($DatasetPaths.Name)，退出码: $LASTEXITCODE"
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
        Ensure-Configured
    }
    "build" {
        Build-Targets -Targets @("icon-recognition-tests")
    }
    "quick" {
        if ($UseLocalExpected) {
            throw "quick 固定校验 Win32/ADB 子模块基线，不支持 -UseLocalExpected。"
        }
        & (Join-Path $testRoot "test_dataset_manifest.ps1")
        Build-Targets -Targets @("icon-recognition-tests")
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
        foreach ($datasetName in @("win32", "adb")) {
            $datasetPaths = Prepare-QuickDatasetInput -Name $datasetName
            Invoke-QuickDataset -DatasetPaths $datasetPaths
        }
    }
    "manual" {
        if (-not $Dataset) {
            throw "manual 任务必须显式指定 -Dataset win32 或 -Dataset adb。"
        }
        if ($All -and ($GridType -or $Image)) {
            Show-Usage
            throw "-All 不能与 -GridType 或 -Image 同时使用。"
        }
        if (-not $All -and -not $GridType -and -not $Image) {
            Show-Usage
            throw "manual 任务必须指定 -All、-GridType 或 -Image。"
        }
        $datasetPaths = Prepare-DatasetInput -Name $Dataset
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
        $arguments += @("--dataset", $Dataset)
        if ($PSBoundParameters.ContainsKey("Debug")) {
            $arguments += "--debug"
        }
        $arguments += @("--rois", $datasetPaths.RoisPath)
        if ($Side -eq "full") {
            $arguments += @(
                "--expected",
                (Resolve-ExpectedResultsPath -DatasetPaths $datasetPaths -UseLocal:$UseLocalExpected)
            )
        }
        elseif ($Side -ne "full") {
            Write-Warning "expected.csv 仅维护 full 基线，显式分侧运行仅作人工审计: $Side"
        }
        & (Find-TestExecutable -Name "icon-recognition-manual-runner") @arguments
        if ($LASTEXITCODE -ne 0) {
            throw "icon-recognition-manual-runner 执行失败，退出码: $LASTEXITCODE"
        }
    }
}
