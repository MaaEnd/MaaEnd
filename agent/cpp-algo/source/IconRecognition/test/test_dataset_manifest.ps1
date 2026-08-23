$ErrorActionPreference = "Stop"

$testRoot = $PSScriptRoot
$repoRoot = (Resolve-Path -LiteralPath (Join-Path $testRoot "../../../../..")).Path
$manifestPath = Join-Path $testRoot "dataset-manifest.psd1"
$requiredDatasets = @("win32", "adb")
$requiredGridTypes = @(
    "trade",
    "transfer",
    "port_storager",
    "valuables",
    "shipment",
    "credit_trade",
    "rewards",
    "single_roi"
)

if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
    throw "IconRecognition dataset manifest is missing: $manifestPath"
}

$manifest = Import-PowerShellDataFile -LiteralPath $manifestPath
$unknownDatasets = @($manifest.Keys | Where-Object { $_ -notin $requiredDatasets })
if ($unknownDatasets.Count -gt 0) {
    throw "Unknown IconRecognition datasets: $($unknownDatasets -join ', ')"
}

foreach ($dataset in $requiredDatasets) {
    if (-not $manifest.ContainsKey($dataset)) {
        throw "IconRecognition dataset is missing: $dataset"
    }
    $config = $manifest[$dataset]
    if ($config.Keys.Count -ne 2 -or -not $config.ContainsKey("Root") -or -not $config.ContainsKey("QuickFixtures")) {
        throw "IconRecognition dataset must define only Root and QuickFixtures: $dataset"
    }
    $root = Join-Path $repoRoot $config.Root
    $expectedPath = Join-Path $root "expected.csv"
    $roisPath = Join-Path $root "rois.json"
    foreach ($path in @($root, $expectedPath, $roisPath)) {
        if (-not (Test-Path -LiteralPath $path)) {
            throw "IconRecognition dataset path is missing: $path"
        }
    }

    $quickFixtures = $config.QuickFixtures
    $unknownGridTypes = @($quickFixtures.Keys | Where-Object { $_ -notin $requiredGridTypes })
    if ($unknownGridTypes.Count -gt 0) {
        throw "Unknown quick grid types for ${dataset}: $($unknownGridTypes -join ', ')"
    }
    $missingGridTypes = @($requiredGridTypes | Where-Object { -not $quickFixtures.ContainsKey($_) })
    if ($missingGridTypes.Count -gt 0) {
        throw "Quick grid types are missing for ${dataset}: $($missingGridTypes -join ', ')"
    }

    $expected = @(Import-Csv -LiteralPath $expectedPath -Encoding UTF8)
    $rois = Get-Content -LiteralPath $roisPath -Raw -Encoding UTF8 | ConvertFrom-Json
    foreach ($gridType in $requiredGridTypes) {
        $fixtures = @($quickFixtures[$gridType])
        if ($fixtures.Count -eq 0) {
            throw "Quick fixtures are empty for ${dataset}/${gridType}"
        }
        if ($gridType -ne "single_roi" -and $rois.PSObject.Properties.Name -notcontains $gridType) {
            throw "ROI is missing for ${dataset}/${gridType}"
        }
        foreach ($fixture in $fixtures) {
            if (-not $fixture.StartsWith("$gridType/", [System.StringComparison]::Ordinal)) {
                throw "Quick fixture uses the wrong grid type: ${dataset}/${fixture}"
            }
            if (-not (Test-Path -LiteralPath (Join-Path $root $fixture) -PathType Leaf)) {
                throw "Quick fixture is missing: ${dataset}/${fixture}"
            }
            $expectedRows = @($expected | Where-Object { $_.image -eq $fixture })
            $expectedCount = ($expectedRows | Measure-Object -Property count -Sum).Sum
            if ($expectedRows.Count -eq 0 -or $expectedCount -le 0) {
                throw "Quick fixture has no positive expected result: ${dataset}/${fixture}"
            }
        }
    }
}

Write-Output "IconRecognition dataset manifest tests passed"
