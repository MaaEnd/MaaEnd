# Nailong Custom Client

The Nailong theme is maintained in the MaaEnd repository and built on a pinned MXU commit. No MXU fork is required, and no changes are written to the official MXU repository.

## Source and Build Entry Points

- `tools/client/nailong/client.json`: official MXU source URL, pinned commit and custom version.
- `tools/client/nailong/theme.patch`: source changes for the theme, cursor tracking, interactive phrases and development demo.
- `tools/client/nailong/assets/`: background, images, animation and image-editing prompts.
- `tools/client/nailong/client.mjs`: source preparation, testing, development and build entry point.
- `.cache/nailong-client/`: local working directory generated from official sources; not committed.

Requirements: Node.js 22 or newer, pnpm, Rust stable and platform build tools. Windows requires Visual Studio C++ desktop tools and Windows SDK; Linux requires WebKitGTK 4.1, AppIndicator, librsvg and patchelf; macOS requires Xcode command-line tools.

Run from the MaaEnd repository root:

```bash
pnpm install --frozen-lockfile
pnpm client:prepare
pnpm client:test
pnpm client:build
```

`client:prepare` prepares the official commit specified by `client.json`, checks and applies the patch, copies assets and stamps the custom version consistently. Repeated runs verify file digests. Edited cache files or changed patches stop preparation and require saving changes and rebuilding the cache; user files are never automatically removed.

`client:build` installs the executable as `install/MaaEnd.exe` on Windows or `install/MaaEnd` on macOS/Linux, together with a `nailong-client.json` version record and a Windows PDB when available. It builds only the interface; real tasks still require MaaFramework, Go/C++ agents and MaaEnd resources.

The client installation step in `setup-workspace` calls this same entry point instead of downloading the official generic MXU binary. Existing Python environment preparation and agent build procedures remain unchanged.

## Development and Demo

```bash
pnpm client:dev
```

The development entry point copies files such as MaaEnd's `assets/interface.json` into an isolated debug directory and links resource directories. Existing `install/agent` and `install/maafw` directories are also linked. Development configuration is stored in `.cache/nailong-client/src-tauri/target/debug/config/` without affecting existing configuration.

Select the fourth theme, “奶龙” (Nailong), in settings. Background opacity is adjustable, and desktop eyes keep tracking the cursor outside the window. Clicking the bottom-right mascot switches between four phrases; connection waiting displays `Thinking...`.

Development builds show a “主题演示” (Theme demo) switch beside the task button. It tests “进入战斗模式” (Enter battle mode), “退出战斗模式” (Exit battle mode) and “模拟完成” (Simulate completion) without the game, elevation or runtime libraries. Demo state exists independently in memory and never changes real connections or task states. Hotkeys, scheduled entry points and the dashboard cannot start real tasks during the demo. Production builds hide the demo entry point.

## Release Integration

`.github/workflows/install.yml` builds the client from MaaEnd's patch and copies it into the existing release directory. Supported targets are Windows/macOS x86_64 and aarch64, plus Linux x86_64. Linux aarch64 retains its existing behavior without a bundled desktop client.

Specify a build target and output directory directly:

```bash
node tools/client/nailong/client.mjs build --target win-x86_64 --output .cache/client-dist
```

Cross-compilation requires installing the corresponding target with `rustup target add`. Release versions come from `client.json` and do not automatically follow the latest MXU release. Check patch applicability before updating the base commit. Cached sources are not the maintained source of truth; theme edits must be written back to the patch and asset directory.

## Pre-commit Validation

This feature does not modify game pipelines or node tests. Run the entire custom client test suite and build before committing:

```bash
pnpm client:test
pnpm client:build
pnpm exec prettier --check tools/client/nailong/client.json package.json
pnpm exec markdownlint docs/zh_cn/developers/nailong-client.md docs/en_us/developers/nailong-client.md
```

`client:test` includes preparation idempotence and cache-preservation tests, all frontend unit tests, TypeScript checking, the production frontend build and all Rust tests. On Windows, also run all four startup regressions and six repeated-startup pairs from the pinned MXU sources in elevated PowerShell 7, using the executable built in this run:

```powershell
$source = '.cache/nailong-client/tests/windows'
$exe = (Resolve-Path 'install/MaaEnd.exe').Path
$evidence = Join-Path $PWD '.cache/nailong-validation'
foreach ($scenario in @('creation-failure', 'normal-close', 'hidden-window', 'minimize-to-tray')) {
    pwsh -NoProfile -File "$source/startup.ps1" -Executable $exe -Scenario $scenario -EvidenceDirectory $evidence
    if ($LASTEXITCODE -ne 0) { throw "Startup regression failed: $scenario" }
}
pwsh -NoProfile -File "$source/repeated-startup.ps1" -Executable $exe -EvidenceDirectory "$evidence/repeated" -Pairs 6
if ($LASTEXITCODE -ne 0) { throw 'Repeated startup regression failed' }
```

Tests operate only on their own test processes. Logs and diagnostic files remain in ignored cache directories and must not be pushed with source code. To remove the customization, revert this MaaEnd change; local caches and original configuration do not need to be deleted.
