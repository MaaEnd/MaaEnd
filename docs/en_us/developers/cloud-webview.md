# Cloud Endfield PC web support

This document describes the Windows adaptation for [Issue #6483](https://github.com/MaaEnd/MaaEnd/issues/6483). The entry point is the unmodified MXU GUI. All compatibility code lives in MaaEnd, without MXU or MaaFramework changes. The feature remains experimental.

## Usage

1. Install the system WebView2 Runtime and select the Cloud Endfield web controller in the existing GUI.
2. Enable `Log in and Start Cloud Endfield Web`, select PC tasks, and start. The pretask opens or reuses the cloud window, checks authentication, starts the official game, and waits for the queue and first frame.
3. If signed out, choose any official login method, including QR code, phone number, password, or verification code. Manual login does not consume the startup timeout. The separate login pretask only allows early login and can be removed afterwards; startup still checks authentication.
4. The existing GUI handles options, presets, logs, and stopping. Task completion or failure releases input and stops bridge updates. Add the cloud controller's `Close Game (PC)` task to close the cloud window; closing the GUI also reclaims its associated cloud window. The pretask reopens it on the next run.

Connections are direct by default and ignore system proxies. A proxy is used only when `MAAEND_CLOUD_PROXY` explicitly specifies a host and port. The independent WebView2 authentication directory is `agent/cloud-webview.exe.WebView2/`; do not read, commit, or distribute it in test packages. Authentication detection examines only public UI, without cookies, tokens, account fields, or form values; unknown state never counts as authenticated.

The cloud host uses the system DNS resolution path through the fixed `--disable-features=AsyncDns` argument, keeping resolution consistent with desktop network configuration without changing system DNS, pinning CDN IP addresses, or relaxing certificate validation. This setting affects only connection setup in the cloud host, adds no game-input waiting, and does not change the zipline-import WebView.

## Compatibility bridge

`Win32-CloudWeb` uses MXU's existing Win32 controller: `PrintWindow` capture and plain `SendMessage` input. `agent/cpp-algo/cloud-webview/` reuses `Common/WebView2` and the system Runtime to host the [official website](https://endfield.hypergryph.com/cloud), exposing a fixed 1280×720 transparent, click-through, nonactivating bridge HWND. The bridge and visible game windows are separate, and its message thread is separate from the WebView STA to avoid synchronous browser call deadlocks.

Game frames come from existing video through WebView2 SharedBuffer, without quality enhancement or PNG encoding and decoding for task captures. The startup first-frame check still converts video to PNG; the task controller is unavailable without decoded video. Transport validates version, dimensions, stride, and RGBA8 layout before converting a read-only mapping to BGR8. Framework's `PrintWindow` reads a Windows composition surface, so the bridge updates its cache and GDI surface at up to approximately 30 fps during tasks, adding pixel conversion and copying overhead. Connection preparation briefly updates the surface; task completion stops updates.

The bridge reuses two image buffers, with capture and painting never sharing writable pixels, avoiding allocation and release on every frame. At most one frame notification remains pending, so a busy message thread paints the newest cached image. In-process keyboard and mouse input calls the host directly, avoiding a queue behind video capture on the external serial pipe. The external protocol remains available, input transactions remain serialized without replay on failure, and the refresh rate is unchanged. These optimizations cannot eliminate `PrintWindow` and Windows composition costs or establish that navigation issues are resolved.

Window messages map to existing CDP input, without `Seize` or variants that reposition the system cursor or windows. The `CloudRelativeMove` Custom Action and C++ navigation callback send 32-bit relative deltas, bypassing the system mouse locking path in Framework's native Win32 relative input. Ordinary PC controllers retain their original path. The existing AgentServer Tasker Sink reports task lifecycle and releases input. The bridge uses Framework task IDs to ignore delayed events, preventing an earlier task's completion from pausing the next task's frame updates.

The GUI may disconnect an Agent before its asynchronous task-end event arrives, so the bridge also watches the current C++ Agent process, releasing input and pausing updates when it exits. Failed startup preparation withdraws the bridge window and closes the host, preventing a continued GUI queue from connecting to an unready image. Abnormal bridge destruction also reclaims the host, while a separate process watch handles GUI exit.

`CloudTransport` and `ViewInput` use standard C++ types; Windows HWND, pipes, and shared mappings stay in platform adapters. Other desktop platforms can implement corresponding transport and input callbacks. No runnable Linux web host exists yet. Android continues to use the existing `CloudADB` client without web resources.

## Languages and platform interfaces

`cloud-webview` is a C++ build target within `agent/cpp-algo`, producing `cloud-webview.exe` on Windows without bundling Python, Node, or another application language runtime. Page DOM checks and input policies use embedded JavaScript executed by the existing WebView2 runtime; build and installation scripts use the repository's existing Python, and policy tests use its existing Node toolchain. Go only adds cloud-specific UID recognition routing and exemptions from local display-setting checks, without taking over game business flow.

Other desktop adapters should use the following boundaries without spreading Windows messages, handles, or process management into navigation algorithms:

1. Implement `CloudTransport` from `transport/cloud_transport.h`: captures return independently owned, top-down BGR8 pixels without row padding; absolute coordinates and relative deltas use the fixed 1280×720 page space. Keys retain the PC Pipeline virtual-key contract, with native platform keycodes converted in the adapter; text uses a separate UTF-8 path. Failed input may already have applied and must not be replayed automatically.
2. Add platform controller detection, the `CreateViewInput` relative-input callback, and `TaskerEvent` / `CloseActionRun` lifecycle adaptation in `Common/CloudViewInput.cpp`. `ViewInput` accepts only signed 32-bit deltas; unsupported controllers return an empty callback and retain their existing platform backend. Non-Windows branches currently lack a web host implementation, so they do not establish Linux support.
3. Provide a bridge between the platform browser host and a controller already supported by the GUI. Handle readiness, task start/stop, input release, close, and process-exit cleanup independently before adding corresponding controller resource bindings. Other platforms do not need to copy `Win32ControllerBridge`, Windows named pipes, or shared mappings. Android does not use this web path.

Portable C++ input-state and startup-notice tests do not depend on Windows, MaaFramework, or WebView2 and can be built separately with CMake 3.28 and a C++20 compiler. They validate interface policies rather than proving platform GUI or live cloud-game support:

```sh
cmake -S agent/cpp-algo/cloud-webview/tests -B build/cloud-policy
cmake --build build/cloud-policy --config Release
ctest --test-dir build/cloud-policy -C Release -L '^cloud-policy$' --no-tests=error --output-on-failure
```

## Windows and resources

Users can focus, unfocus, minimize, maximize, and resize the visible window; tasks should not restore it or reclaim focus. The page retains a fixed 1280×720 CSS viewport and DPR, scales proportionally, and preserves its valid viewport while minimized. Captures and input always use video coordinates instead of desktop coordinates affected by system scaling.

Before website scripts load, policy rejects `requestPointerLock` and prevents `window.focus()` activation. A transparent child blocks physical hover and scrolling while unfocused, and deliberate clicks enable manual input. Relative input uses `movementX/Y`, split to respect official SDK limits, without moving the system cursor. Mouse reset avoids the official floating control; cloud scene rules move away from overlays before Esc, and middle-button browser autoscrolling is disabled.

The official SDK also treats ordinary mouse `movementX/Y` as camera deltas. Before dispatching an absolute move, the controller prepares zero deltas for only the next trusted event at the matching coordinates. Unpressed positioning suppresses unintended turning, while dragging with a button held retains browser deltas so the website does not discard zero-delta slider or map drags. Relative turning uses its explicitly requested deltas. Failed dispatch clears the pending state without replay. Win32 drag messages also update every held button's coordinates so release uses the endpoint. Real user movement retains browser values. This adds one page call to absolute moves without changing the number of calls for relative input. Page-policy tests cover absolute positioning, dragging, relative turning, unrelated events, and manual input after cancellation.

Automated mouse input checks the actual DOM hit target. Only when the official floating menu covers playing video is its container temporarily made `inert`; CDP input keeps its original coordinates and restores the prior state afterwards. A relative-input batch shares one guard. Unobstructed input does not change the menu or add a restoration call. Official controls remain available on startup, paused, and ended pages, and users can still operate the menu manually. Video frames do not contain the official floating menu, so task captures alone cannot establish whether DOM intercepted a click.

Horizontal web-input units differ from native PC units. Stationary bidirectional measurements showed nearly twice the requested angle with PC defaults, so cloud navigation starts with 4 units per degree horizontally. The upstream sensitivity observer can still correct differences caused by user settings. This initial value applies only when a cloud bridge is detected, without changing ordinary PC input, absolute click coordinates, manual input, or upstream correction thresholds. Pitch retains its existing value pending independent measurement.

Measurements use the camera heading `camRot` estimated from the upper-left minimap by upstream `MapLocateRecognition`, rather than the character-arrow heading `rot`, without reading game memory. With the character stationary, each known horizontal input is followed by a signed circular difference between headings before and after the input. For example, a 120-unit input changed the estimate from 14.933613° to 44.199921°, approximately 29.27°; the PC default of approximately 7.927778 units per degree predicts approximately 15.14°, giving a ratio of approximately 1.93. The median ratio across 16 bidirectional samples was approximately 1.94. This is an image-based measurement of the current web input path, not a fixed CDP coefficient; model error, frame stability, and user settings limit its precision.

These units belong to the fixed page input space, rather than physical host-window pixels. The official SDK converts horizontal deltas using `round(movementX × gameWidth / round(screen.width × devicePixelRatio) × SDK mouse multiplier)`; the host fixes that denominator at 1280. Automated horizontal turning therefore uses `target angle × 4 × existing sensitivity correction`, without another factor for desktop resolution, window width, or Windows scaling at 100%/125%/150%. Presentation separately uses `s = 16 × max(1, min(floor(client width / 16), floor(client height / 9))) / 1280`, applied to both native WebView rasterization and CDP display scaling; this is not an additional turning factor. Changes to the page CSS viewport, DPR, or SDK input mapping require this boundary to be verified again rather than reusing the initial value unchanged.

The web controller loads PC `resource` and appends only `resource_cloud_web`, without Android resources. Shared business Pipelines and Go Service retain their flows. Automatic gathering reuses PC localization, turning, and pickup. Yellow adjustments remain specific to cloud resource nodes, without global recoloring or threshold relaxation. Zipline import still uses a separate WebView in `cpp-algo`, unaffected by cloud window policy and lifecycle.

The startup pretask recovers public website states: pending previous-session data requires confirming that message's own 「知道了」 button before requesting startup from the landing page. After document loading completes, an explicit bootstrap script failure permits one reload; if the dependency still fails, one further reload bypasses the browser cache, after which another failure exits. Only these failure paths back off for 1 and 2 seconds respectively, continuously checking page state and canceling the reload if the website recovers itself or enters another state such as login, to avoid immediately hitting the same network failure burst. Resources successfully retried by the website clear their corresponding stale errors; stale resource errors do not reload loading, login, queue, or game-starting states. Idle-ended and queue-timeout notices are each confirmed once. When the message precedes an actionable button, readiness has a fixed 10-second deadline, followed by a separate 10-second dismissal deadline after confirmation. Polling extends neither deadline, and repeated notices fail explicitly. Diagnostics contain only public bootstrap resource paths, Chromium errors, and allowlisted metadata such as response types, never request headers, response bodies, or account/session information. Recovery does not disable browser security checks, clear authentication, replay input, or alter game business flow, and adds no waiting to normal game input.

## Building and verification

Prepare dependencies as described in [development tools](./tools-and-debug.md), then build:

```powershell
uv run build-and-install --cpp-algo
```

For development, build `cloud-webview` separately. Set `MAAEND_CLOUD_SOFTWARE_RENDERING=1` before creating the host to disable GPU composition and hardware decoding; close existing cloud windows before switching. Software mode retains the controller and resources, with CPU cost dependent on video and hardware; this does not establish support for every GPU-less device.

Portable input and startup policies can be checked with the standalone CMake commands above. Configuring the full Agent project with `MAAEND_BUILD_CLOUD_WEBVIEW_TESTS=ON` also provides Windows bridge tests using the original Toolkit and Framework:

```sh
# Run in the build directory; adjust -C for the actual multi-configuration build.
ctest -C Release -L '^cloud-policy$' --no-tests=error --output-on-failure
ctest -C Release -L '^cloud-desktop$' --no-tests=error --output-on-failure
# Run in the repository root.
node --test agent/cpp-algo/cloud-webview/tests/*.test.mjs
uv run python -m unittest tools.setup.test_setup_workspace
```

`cloud-desktop` requires an idle interactive desktop with DWM enabled; do not move the mouse or switch focus while it runs. It covers enumeration, changing captures, clicks, Esc, Unicode, drag endpoints, relative input, stop-time release, and restart, without replacing live website tests. Live tasks must connect the existing Go Service as well.

Development setup remains compatible with older upstream C++ Agent artifacts that omit the cloud host. Artifacts containing `cloud-webview.exe` must also provide `WebView2Loader.dll` and `MaaUtils.dll`; missing dependencies fail validation before any files are replaced.

## Verification limits

Live retesting of the current compatibility bridge completed Common Route 1 twice consecutively and Common Route 2 once, reaching the destination and closing the cloud window naturally. Common Route 2 included minimizing, maximizing, restoring while unfocused, and physical pointer traversal. The floating-menu fix for leaving the reward page passed isolated WebView2 input regression. These results cover specific routes and input scenarios rather than establishing that the full daily run passed; results from the old standalone task GUI cannot replace validation of the current compatibility bridge either.

Sustained real 125%/150% DPI, other desktop resolutions, multiple displays, QQ/WeChat screenshots, and low-end CPUs still need testing. Samples that briefly reach high DPI before falling back to 96 are invalid; check actual window DPI for every formal sample. Turn angles come from minimap model estimates and retain residual error, so another unverified fixed multiplier must not be added.

The bridge incurs continuous refresh, pixel conversion, and `PrintWindow` composition-surface costs. Buffer reuse and direct input optimize their respective stages without establishing improvements in total recognition latency, CPU usage, or navigation stability. The final package needs separate checks of changing frames, idle cost, input latency, window interaction, cancellation, restart, and GUI-exit cleanup.

Recognition regression cases use the `tests/MaaEndTestset` submodule. Coordinate pending companion fixtures before merging the main repository; missing cloud images must not be silently skipped. Do not commit or distribute screenshots, runtime logs, or browser data containing other windows, UIDs, or account information.
