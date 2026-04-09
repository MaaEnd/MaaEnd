---
name: windows-dmp-analysis
description: 分析 Windows 崩溃转储文件（.dmp），诊断 MaaEnd 及其依赖项（MaaFramework、MXU）的崩溃。自动从 GitHub Releases 下载对应版本 PDB 符号，使用 minidump-stackwalk 解析堆栈轨迹并定位崩溃根因。当 issue 日志包或附件中发现 .dmp 文件，或用户要求分析 DMP/崩溃转储时使用。
---

# Windows DMP Analysis

## Scope

- Windows minidump (.dmp) files from MaaEnd.
- `MaaEnd.dmp` crashes almost always originate in **MaaFramework** (C++) or **MXU** (Rust/Tauri), not MaaEnd's Go code.
- Only x86_64 covered below; for aarch64, substitute `x86_64` → `aarch64` in all download URLs.

## Prerequisites

在本仓库的 `issue-ai-analysis` workflow 中会安装并确保 `minidump-stackwalk` 和 `dump_syms` 可用（见 `.github/workflows/issue-ai-analysis.yml`）。如果你在本地复现或手动运行本流程，需要自行安装这些工具（可参考下文安装命令）。

Verify before proceeding:

```bash
which minidump-stackwalk && which dump_syms
```

If either is missing, install via:

```bash
curl -L --proto '=https' --tlsv1.2 -sSf https://raw.githubusercontent.com/cargo-bins/cargo-binstall/main/install-from-binstall-release.sh | bash
cargo binstall -y --no-confirm minidump-stackwalk dump_syms
```

## Workflow

### 1. Obtain DMP

Download `.dmp` to `.cache/dmp-analysis/issue-<number>/`.

Sources:

- Direct issue attachment (image/file link ending in `.dmp`)
- Inside `MaaEnd-logs-*.zip` log package

```bash
WORK=".cache/dmp-analysis/issue-<NUMBER>"
mkdir -p "$WORK"
curl -L "<dmp_url>" -o "$WORK/MaaEnd.dmp"
```

### 2. Quick unsymbolicated analysis

```bash
minidump-stackwalk "$WORK/MaaEnd.dmp" 2>/dev/null
```

This gives without any symbols: OS info, exception type, module list with versions, raw stack frames.

Identify the **crashing module** from the exception address or the top stack frame.

### 3. Correlate DMP with logs

The DMP filename often contains the crashing PID (e.g. `MaaEnd.exe.18188.dmp` → PID 18188).
Match this with `maa.log` entries tagged `[Px18188]` to pinpoint the exact crashing session and its timeline.

### 4. Determine dependency versions

DMP module version info is frequently empty/unavailable. Prefer log and config sources:

| Priority | Source                          | How                                                             |
| -------- | ------------------------------- | --------------------------------------------------------------- |
| 1        | `mxu-tauri.log`                 | `maa_init success, version: v5.x.x`                             |
| 2        | `go-service.log`                | `client_maafw_version` / `client_version` in PI environment log |
| 3        | Config files from logs package  | `interface.json`, `maa_option.json`                             |
| 4        | Issue text                      | User-reported version                                           |
| 5        | Module list in stackwalk output | Version column (often shows `?`)                                |

Record:

- **MaaFramework version** (e.g. `5.9.2`)
- **MXU version** (e.g. `1.21.2`)

### 5. Download PDB symbols

#### MaaFramework

```bash
MAA_VER="<version>"   # e.g. 5.9.2
curl -sL "https://github.com/MaaXYZ/MaaFramework/releases/download/v${MAA_VER}/MAA-win-x86_64-v${MAA_VER}.zip" \
  -o "$WORK/maa-fw.zip"
unzip -joq "$WORK/maa-fw.zip" 'symbol/*.pdb' -d "$WORK/pdb/"
```

PDB files inside `symbol/`:

| PDB                     | Corresponding Module                                    |
| ----------------------- | ------------------------------------------------------- |
| MaaFramework.pdb        | MaaFramework.dll — core pipeline runtime                |
| MaaUtils.pdb            | MaaUtils.dll — utility library                          |
| MaaToolkit.pdb          | MaaToolkit.dll — toolkit                                |
| MaaWin32ControlUnit.pdb | MaaWin32ControlUnit.dll — Win32 controller              |
| MaaAdbControlUnit.pdb   | MaaAdbControlUnit.dll — ADB controller                  |
| MaaAgentServer.pdb      | MaaAgentServer.dll — agent server                       |
| MaaAgentClient.pdb      | MaaAgentClient.dll — agent client                       |
| MaaPiCli.pdb            | MaaPiCli.exe — CLI entry                                |
| Others                  | GamepadControlUnit, CustomControlUnit, NodeServer, Node |

#### MXU

```bash
MXU_VER="<version>"   # e.g. 1.21.2
curl -sL "https://github.com/MistEO/MXU/releases/download/v${MXU_VER}/MXU-win-x86_64-v${MXU_VER}.zip" \
  -o "$WORK/mxu.zip"
unzip -joq "$WORK/mxu.zip" 'mxu.pdb' -d "$WORK/pdb/"
```

`mxu.pdb` is at the zip root (≈230 MB).

### 6. Convert PDB → Breakpad .sym

```bash
mkdir -p "$WORK/symbols"
for pdb in "$WORK/pdb/"*.pdb; do
  name=$(basename "$pdb" .pdb)
  header=$(dump_syms "$pdb" 2>/dev/null | head -1)
  debug_id=$(echo "$header" | awk '{print $4}')
  dest="$WORK/symbols/${name}.pdb/${debug_id}"
  mkdir -p "$dest"
  dump_syms "$pdb" > "$dest/${name}.sym" 2>/dev/null
done
```

### 7. Full symbolicated stack walk

```bash
minidump-stackwalk "$WORK/MaaEnd.dmp" "$WORK/symbols" 2>/dev/null
```

Now stack traces include function names, source paths, and line numbers.

### 8. Analyze results

#### What to focus on

1. **Crashing thread** — read stack top-down.
2. **Exception type**:
    - `EXCEPTION_ACCESS_VIOLATION` (0xC0000005) — null/dangling pointer, use-after-free
    - `EXCEPTION_STACK_OVERFLOW` (0xC00000FD) — infinite recursion or oversized stack allocation
    - `EXCEPTION_ILLEGAL_INSTRUCTION` (0xC000001D) — corrupted code or wrong CPU feature
    - `STATUS_STACK_BUFFER_OVERRUN` (0xC0000409) — **NOT always a real buffer overrun.** This is the Windows fast-fail mechanism. Check the first exception parameter:
        - `0x7` = `FAST_FAIL_FATAL_APP_EXIT` — means `std::terminate()` / `abort()` was called, typically from an **unhandled C++ exception** (e.g. `cv::Exception` from OpenCV receiving an empty `cv::Mat`). This is the most common MaaEnd crash pattern.
        - `0x2` = `FAST_FAIL_RANGE_CHECK_FAILURE`
        - Other values: see [FAST_FAIL codes](https://learn.microsoft.com/en-us/windows/win32/debug/fast-fail-constants)
    - `EXCEPTION_BREAKPOINT` (0x80000003) — deliberate crash / assertion failure / Rust panic
3. **Faulting module ownership**:
    - `Maa*.dll` → MaaFramework → upstream `MaaXYZ/MaaFramework`
    - `mxu.exe` → MXU → upstream `MistEO/MXU`
    - `onnxruntime_maa.dll`, `opencv_world4_maa.dll`, `fastdeploy_ppocr_maa.dll` → third-party inference/vision
    - `DirectML.dll` → DirectX ML runtime
    - `ViGEmClient.dll` → virtual gamepad
    - `ntdll.dll`, `KERNELBASE.dll`, `ucrtbase.dll` → OS / CRT; look at the caller frames above
    - If crash address is in `ucrtbase.dll` with code 0xC0000409, the real crash site is in the **caller frames**, not ucrtbase itself
4. **Recurring patterns**:
    - Multiple threads crashing or deadlocked
    - Stack corruption (truncated / nonsensical frames)
    - Heap corruption indicators (`RtlReportCriticalFailure`, `RtlpLogHeapFailure`)
    - Rust panic (`rust_begin_unwind`, `core::panicking::*`)

### 9. Cross-reference with source

If the crash is in MaaFramework:

```bash
git clone --depth 1 --branch "v${MAA_VER}" \
  https://github.com/MaaXYZ/MaaFramework.git ".cache/upstream-src/MaaFramework"
```

If the crash is in MXU:

```bash
git clone --depth 1 --branch "v${MXU_VER}" \
  https://github.com/MistEO/MXU.git ".cache/upstream-src/MXU"
```

Look up the function and line from the symbolicated stack trace in the cloned source.

## Output Format

```markdown
## DMP 分析结果

- DMP 文件：`<filename>`
- 操作系统：`<OS version>`
- 异常类型：`<EXCEPTION_*>`
- 崩溃模块：`<module_name>` (版本 `<version>`)
- 崩溃函数：`<symbolicated function name>`

## DMP 崩溃分析

### 崩溃堆栈（crashing thread）

<crashing thread 的全部有效符号化堆栈帧；如堆栈被截断/损坏，请在此说明原因>

### 关键模块版本

| Module           | Version |
| ---------------- | ------- |
| mxu.exe          | ...     |
| MaaFramework.dll | ...     |
| ...              | ...     |

### 根因判断

- 崩溃归属：MaaFramework / MXU / 第三方依赖 / 未知
- 分析：...
- 置信度：高 / 中 / 低

### 建议

- 对用户的建议（升级、绕过方案等）
- 对开发者的建议（上游报告、修复方向）
```

## Cleanup

After analysis is complete:

```bash
rm -rf ".cache/dmp-analysis/issue-<NUMBER>"
```
