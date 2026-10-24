# Cloud Endfield PC web support

The Windows web adapter uses unmodified MXU and MaaFramework; all changes remain in MaaEnd. This is still a compatibility solution and does not satisfy the review request for a direct CDP controller.

## Usage

1. Install the system WebView2 Runtime, select the Cloud Endfield web controller in the GUI, and enable its launch pre-task.
2. The pre-task only opens or reuses the window and binds its lifetime to the GUI. Daily tasks use `CloudWebPrepare` in cloud resources to check authentication, start the game, and wait for queueing and decoded video.
3. When signed out, choose any authentication method offered by the official page. Waiting for manual login does not consume the startup timeout. An optional login task confirms authentication without starting the game and can then be removed.
4. Stop, completion, and failure release keys and mouse buttons. The cloud-only close task closes the window; GUI exit also reclaims it. The launch pre-task opens it again for the next run.

Connections bypass system proxies by default. Only explicitly setting `MAAEND_CLOUD_PROXY=host:port` enables a proxy. `MAAEND_CLOUD_SOFTWARE_RENDERING=1` disables GPU composition and hardware video decoding; close the existing cloud window before switching. CPU cost depends on the video and hardware; this does not establish support for every device without a GPU.

Login data remains in `agent/cloud-webview.exe.WebView2/`, separate from the zipline import window. Do not inspect, commit, or distribute this directory. Authentication checks inspect public DOM state without reading cookies, tokens, account fields, or login form values.

## Responsibilities and interfaces

The browser host is a separate process mode of the existing `cpp-algo`, with no additional `main.cpp` or standalone host executable. `CloudHost.cpp` only handles processes, windows, and GUI exit cleanup. Initialization timeout cleanup uses the handle of the newly created process rather than relying on an unpublished window title.

Login, game startup, queueing, and notice recovery live in `resource_cloud_web/pipeline/SceneManager/SceneCloudWeb.json`. The `CloudPageState` Custom Recognition queries public state and hit-tested button positions; `CloudPageReload` only reloads the same document if its recognized failure still exists. Each operation is followed by recognition. Ended-session and queue-timeout notices are each confirmed once; missing buttons, failed dismissal, and repeated notices fail. The previous-session saving notice has a separate bounded backoff, without adding delays to normal input.

Page scripts live in `cloud-webview/scripts/`, are embedded by CMake, and are loaded directly by tests. The application retains the existing C++, Go, and WebView2 runtimes, without distributing Node, Python, or another application language runtime. JavaScript only runs inside WebView2.

`CloudTransport` returns independently owned, top-down BGR8 pixels without row padding. Input uses a fixed 1280×720 page space, PC virtual-key conventions, and a separate UTF-8 text path. `ViewInput` accepts signed 32-bit relative deltas only. Windows HWND, pipe, shared mapping, and lifecycle code stay in platform adapters, leaving replaceable interfaces for other desktop platforms. No runnable Linux web host exists yet; Android retains `CloudADB`.

## Compatibility bridge and cost

The stock GUI PI lacks an arbitrary Custom Controller entry, and a remote Agent cannot replace the GUI Tasker's controller. `Win32-CloudWeb` therefore still uses a transparent, nonactivating, input-transparent HWND to support `PrintWindow` and `SendMessage`. Its message thread is separate from the WebView STA, and input calls CDP directly inside the host. The external protocol remains available, without replaying failed input that may already have applied.

Game capture follows `video → Canvas → WebView2 SharedBuffer → BGR → GDI → PrintWindow`, without depending on quality enhancement or encoding PNG for every game frame. Pixel conversion, copies, and a second bitmap read remain; this is neither zero-copy nor a controller returning BGR directly. Before decoded video exists, page PNG capture lets the GUI connect and run startup Pipeline. That path runs at no more than 2 fps and is not the normal game capture path.

Windows `PrintWindow` can read DWM's cache without sending `WM_PRINT`, so the existing interface cannot guarantee that every request synchronously triggers browser capture. Active tasks refresh at up to approximately 30 fps; stopped or completed tasks retain 2 fps updates so previews do not freeze permanently. This retains some idle cost, and idle screenshots can lag by one refresh period. Strict capture on demand and removal of the GDI stage remain limited by the current GUI interface.

The bridge reuses two frame buffers and retains at most one pending frame notification. Task IDs and process watches prevent delayed completion events from reducing the next task's refresh rate. Input release does not depend on screenshot updates.

## Input and display

The visible window supports user-controlled focus, minimization, maximization, and resizing. Automation does not restore it or move the system cursor. A fixed CSS viewport and DPR keep display scaling separate from input coordinates and turn sensitivity. Physical hover and wheel input are blocked when unfocused, while explicit clicks enable manual interaction. The cloud window rejects pointer lock, active `window.focus()`, and clipboard-read permission without affecting zipline import.

Absolute positioning suppresses unwanted camera deltas while held-button dragging preserves browser motion; relative input uses explicit deltas. Only automation blocked by the official floating menu temporarily disables that menu's DOM hit testing, restoring it after dispatch. Manual input is unaffected. Wheel input retains its target coordinates; numpad keys and left/right modifiers have separate mappings. Unsupported keys warn without closing the host.

Cloud navigation starts with 4 horizontal units per degree, only for this controller, while retaining the upstream sensitivity observer. Measurements use the upstream minimap model's camera heading `camRot`: one 120-unit sample turned approximately 29.27°, whereas the PC default of approximately 7.927778 units per degree predicts 15.14°, a ratio near 1.93. The median ratio across 16 bidirectional samples was approximately 1.94. This is an image measurement, not a fixed CDP coefficient; model residuals remain, and public thresholds and user DPI are unchanged.

The web adapter uses PC resources plus `resource_cloud_web`. Public PC business flow, routes, and thresholds remain unchanged; yellow-color adjustments are restricted to cloud nodes supported by existing evidence.

## Build and validation

Prepare dependencies using the [development environment guide](./tools-and-debug.md), then build the existing Agent:

```powershell
uv run build-and-install --cpp-algo
```

The C++ Agent and companion DLLs are replaced as one group. A locked file or copy failure restores the old group; failed rollback retains backups and reports their path.

```sh
node --test agent/cpp-algo/cloud-webview/tests/*.test.mjs
uv run agent/cpp-algo/cloud-webview/tests/test_startup_pipeline.py
uv run python -m unittest tools.setup.test_setup_workspace
cmake -S agent/cpp-algo/cloud-webview/tests -B build/cloud-policy
cmake --build build/cloud-policy --config Release
ctest --test-dir build/cloud-policy -C Release -L '^cloud-policy$' --no-tests=error --output-on-failure
```

With `MAAEND_BUILD_CLOUD_WEBVIEW_TESTS=ON`, the full Agent build also supports `ctest -C Release -L '^cloud-desktop$' --no-tests=error --output-on-failure`. It uses stock Framework and Toolkit on an idle interactive desktop with DWM, covering changing frames, throttled idle updates, wheel input, keys, dragging, stop release, and restart. Startup Pipeline tests run actual nodes in Framework with browser state supplied by an isolated test controller; they do not replace official-site testing.

Sustained 125%/150% DPI, multiple monitors, third-party screenshots, low-end CPUs, and full daily tasks under the current architecture require separate validation. Existing route samples and tests of the former standalone GUI do not validate the refactor. Test images come from the `tests/MaaEndTestset` submodule; do not commit screenshots or runtime logs containing other windows, UIDs, or account information.
