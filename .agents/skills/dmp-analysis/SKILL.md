---
name: windows-dmp-analysis
description: 分析 Windows 崩溃转储文件（.dmp），诊断 MaaEnd 及其依赖项（MaaFramework、MXU）的崩溃。自动从 GitHub Releases 下载对应版本 PDB 符号，使用 minidump-stackwalk 解析堆栈轨迹并定位崩溃根因。当 issue 日志包或附件中发现 .dmp 文件，或用户要求分析 DMP/崩溃转储时使用。
---

# Windows DMP Analysis

## Scope

- Windows minidump (.dmp) files from MaaEnd.
- `MaaEnd.dmp` crashes almost always originate in **MaaFramework** (C++) or **MXU** (Rust/Tauri), not MaaEnd's Go code.
- MaaEnd 自有 agent 是独立进程，崩溃时产生各自的 DMP：`cpp-algo.exe.<pid>.dmp`、`go-service.exe.<pid>.dmp`，符号来自 MaaEnd 自己的 Actions artifact（见 5.3 节，不随 release 发布）。
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
Match this with `maafw.log` entries tagged `[Px18188]` to pinpoint the exact crashing session and its timeline.

### 4. Determine dependency versions

DMP module version info is frequently empty/unavailable. Prefer log and config sources:

| Priority | Source | How |
| --- | --- | --- |
| 1 | `mxu-tauri.log` | `maa_init success, version: v5.x.x` |
| 2 | `go-service.log` | `client_maafw_version` / `client_version` in PI environment log |
| 3 | Config files from logs package | `interface.json`, `maa_option.json` |
| 4 | Issue text | User-reported version |
| 5 | Module list in stackwalk output | Version column (often shows `?`) |

Record:

- **MaaEnd version** (e.g. `v0.8.0`; used to fetch MaaEnd symbols in 5.3)
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

| PDB | Corresponding Module |
| --- | --- |
| MaaFramework.pdb | MaaFramework.dll — core pipeline runtime |
| MaaUtils.pdb | MaaUtils.dll — utility library |
| MaaToolkit.pdb | MaaToolkit.dll — toolkit |
| MaaWin32ControlUnit.pdb | MaaWin32ControlUnit.dll — Win32 controller |
| MaaAdbControlUnit.pdb | MaaAdbControlUnit.dll — ADB controller |
| MaaAgentServer.pdb | MaaAgentServer.dll — agent server |
| MaaAgentClient.pdb | MaaAgentClient.dll — agent client |
| MaaPiCli.pdb | MaaPiCli.exe — CLI entry |
| Others | GamepadControlUnit, CustomControlUnit, NodeServer, Node |

#### MXU

```bash
MXU_VER="<version>"   # e.g. 1.21.2
curl -sL "https://github.com/MistEO/MXU/releases/download/v${MXU_VER}/MXU-win-x86_64-v${MXU_VER}.zip" \
  -o "$WORK/mxu.zip"
unzip -joq "$WORK/mxu.zip" 'mxu.pdb' -d "$WORK/pdb/"
```

`mxu.pdb` is at the zip root (≈230 MB).

#### MaaEnd（自有符号）

MaaEnd 构建产出独立符号包，**仅存 Actions artifact（默认保留 90 天），不随 release 发布**。artifact 名即版本：`MaaEnd-pdb-win-<arch>-<tag>`，按 `client_version` 精确匹配：

```bash
MAAEND_VER="<version>"   # e.g. v0.8.0，与 interface.json 中 version 一致
# 1) 按版本号定位最新 artifact id（--paginate 翻页；同名多次构建时按 created_at 取最新；若返回空说明已过期）
ART_ID=$(gh api --paginate "repos/MaaEnd/MaaEnd/actions/artifacts?per_page=100" \
  --jq '.artifacts[] | select(.name == "MaaEnd-pdb-win-x86_64-'"${MAAEND_VER}"'") | [.id, .created_at] | @tsv' \
  | sort -k2 | tail -1 | cut -f1)
[ -n "$ART_ID" ] || { echo "symbol artifact expired or not found (90-day retention)"; exit 1; }
# 2) 下载并解压（GH_TOKEN 需有 actions:read 权限）
curl -sL -H "Authorization: Bearer $GH_TOKEN" \
  "https://api.github.com/repos/MaaEnd/MaaEnd/actions/artifacts/$ART_ID/zip" \
  -o "$WORK/maaend-pdb.zip"
unzip -joq "$WORK/maaend-pdb.zip" -d "$WORK/maaend-pdb/"
```

> artifact 过期后无法再下载，需在对应版本提交上本地构建生成符号（`uv run build-and-install --cpp-algo`）。注意：本地构建的 PDB GUID 与用户崩溃时的二进制不一致，只能近似符号化（函数级）；且该命令只构建 cpp-algo，go-service 需另行执行 Go 构建。

包内内容：

| 文件 | 对应模块 | 用途 |
| --- | --- | --- |
| `cpp-algo.pdb` | `cpp-algo.exe`（C++ 自定义识别/动作 agent，独立进程） | dump_syms → .sym（与 MaaFramework PDB 流程相同） |
| `go-service.exe` | `go-service.exe`（Go agent，独立进程） | **Go 工具链不产出 PDB**；该二进制为未剥离 DWARF 的精确构建产物，即调试符号本体 |
| `symbols-manifest.json` | — | tag / commit / 上游版本，用于版本精确配对与校验 |

- 崩溃模块为 `cpp-algo.exe`：按第 6 节流程转换 `cpp-algo.pdb`。
- 崩溃模块为 `go-service.exe`：minidump-stackwalk 无法直接符号化 Go 二进制。从无符号输出中取栈帧地址（格式 `go-service.exe + 0xXXXX`，模块内相对偏移 RVA），**推荐用 gdb 解析**（自动处理重定位，无需手工换算）：

  ```bash
  gdb -batch -ex "core-file $WORK/MaaEnd.dmp" -ex "bt" "$WORK/maaend-pdb/go-service.exe"
  ```

  或用 `go tool addr2line`（注意：无 `-e` 参数，地址从 stdin 读取，且必须传完整虚拟地址）。Go amd64 PE 默认 image base 为 `0x140000000`，栈帧里的 RVA 需先换算成完整 VA 再传入：

  ```bash
  # 0xXXXX 替换为栈帧中的 RVA；换算后再解析（实测确认：传纯 RVA 会得到 ? / ?:0）
  printf '0x%x\n' $((0x140000000 + 0xXXXX)) | go tool addr2line "$WORK/maaend-pdb/go-service.exe"
  ```

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
    - `cpp-algo.exe` → MaaEnd 自有 C++ agent → 按 5.3 节从 Actions artifact 获取对应 tag 的符号包
    - `go-service.exe` → MaaEnd 自有 Go agent → 同上，用包内精确二进制 + addr2line/gdb
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
