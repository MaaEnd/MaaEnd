# Endfield PC Cloud Web Support

This document describes the Windows web implementation for [Issue #6483](https://github.com/MaaEnd/MaaEnd/issues/6483). Cloud automation uses a single background input design, with window visibility controlled by the user. This remains experimental. The modified companion MXU controller connects to the WebView host through a local named pipe and supplies shared video frames and CDP input to the existing Tasker.

The GUI integration currently depends on [MXU PR #395](https://github.com/MaaXYZ/MXU/pull/395) and is not yet part of the generic client. Building only this repository's host and resources does not enable the controller in a GUI without the companion changes; custom-controller discovery and lifecycle contracts still require coordination between the two repositories.

## Usage

1. Install the system WebView2 Runtime and select the `Cloud Endfield Web (Experimental)` controller.
2. Add the `Log in and Start Cloud Endfield Web` pretask. It opens or reuses the dedicated WebView and checks the existing login state. If signed out or the session has expired, choose any login method offered by the official website, such as QR code, phone number, password, or verification code. The program waits for the signed-in interface without filling or storing credentials. Manual login does not consume the website startup timeout, and the task can be cancelled.
3. After authentication, the same pretask automatically clicks the official start button and waits through the queue until the first video frame is ready. An unknown state must not proceed as signed in. The full and quick daily presets declare this pretask, filtered by controller in the client. The separate `LoginCloudWeb` entry remains compatible with existing configurations; new configurations only need `LaunchCloudWeb`, without adding another task after login.
4. Run a task to enter the PC login flow. Scene management retains the website start-button recognition as a recovery entry when the site returns to its landing page.

The host provides `--login` (wait for manual login), `--prepare` (launch, check login, and wait for game video), `--status` (return state without account identifiers), and `--close` (close the dedicated window normally and wait for process exit). Closing the window preserves the login session in its separate browser directory and does not log out of the website. The companion MXU acquires a host run token before posting tasks. Queue completion closes the host by PID and token; cancellation waits for the controller to release input first. Stale requests with a different token cannot close a new run. `--close` remains an explicit user command.

Account detection inspects only the official public interface. The 「用户中心」 label also exists while signed out and cannot prove authentication. The current positive signals are the account dropdown structure shown only when signed in, or an active game video; a login form takes precedence as signed out. Website changes that prevent verification return unknown. Cookies, tokens, account fields, and form values are not read.

The Cloud Web controller dispatches input through CDP and reads 1280×720 RGBA frames from the existing game video through WebView2 SharedBuffer, then encodes PNG screenshots for MaaFramework. It does not depend on system focus, move the system pointer, or activate or restore the window. The cloud game process connects directly and overrides inherited browser proxy arguments.

## Window and Input Behavior

The background controller uses the same input mode whether the window is in the foreground or background. Users may change focus, minimize, maximize, and resize the window; these actions must not fail a task or cause automation to restore the window or take focus back. Launching an existing host only confirms that it exists and preserves the user's window state. Initial display does not explicitly activate the window; users can still activate the page through the taskbar, Alt+Tab, or a click.

Before website scripts run, the host installs an input policy that rejects `requestPointerLock` and prevents `window.focus()` from taking focus. Background clicks therefore cannot lock or pull back the desktop pointer, while WebView2 continues to handle user clicks and input-field focus normally. `Emulation.setFocusEmulationEnabled` keeps the page active without changing operating-system focus. Relative turns still dispatch DOM `movementX/Y` to the video element, without falling back to system pointer locking.

While inactive, a native transparent child window blocks physical pointer hover and wheel events. A complete deliberate click enables manual input; losing focus restores the shield. Keyboard capture restores the streaming page's local input state, and middle clicks suppress browser autoscroll so it cannot consume the next Esc. Relative movement is attached to trusted CDP mouse events and partitioned to respect the official streaming SDK's 127-pixel per-event limit, without moving the system pointer.

The window retains its system title bar, resize borders, and maximize support. The page keeps a 1280×720 layout and scales proportionally, leaving borders where necessary; minimizing preserves the last valid page viewport. Background screenshots and input coordinates remain based on the game video, rather than switching to desktop coordinates when the outer window changes size.

Browser zoom stays at 1. CDP fixes page dimensions, screen dimensions, and DPR; only `viewport.scale` fits the outer window. Before video becomes available, website screenshots use `Page.captureScreenshot` with an explicit 1280×720 CSS clip and surface capture. Omitting the clip crops scaled content; capturing the native view instead returns a black image when minimized. When the website reports that previous-session data is still being processed, status returns `session_busy`. The startup pretask confirms that notice's own clickable 「知道了」 button, waits for the landing page, and requests startup again at five-second intervals within a 60-second recovery budget. Once queuing or startup begins, the existing waiting behavior resumes. The controller does not treat the saving notice as an actionable game screenshot.

An explicit resource failure for the official account SDK, React, ReactDOM, or website bootstrap scripts, with no login form, authentication signal, or game video, produces `startup_error`. Login and launch pretasks reload once and wait for the new document to replace the old one. A repeated failure exits explicitly without starting daily tasks or clearing the login directory. The host records only these public script URLs without query strings and Chromium network errors, never account APIs, request headers, or bodies. Normal loading, manual login, active games, unrelated resource errors, and read-only `--status` never trigger this recovery.

## CPU-only Mode

Set `MAAEND_CLOUD_SOFTWARE_RENDERING=1` before launching the GUI. The test package also supplies `Start-MaaEnd-CPU.bat`. The variable takes effect only when the host browser is created; close the existing window and GUI normally before changing modes. The normal launcher retains WebView2's default hardware selection.

Software mode explicitly passes `--disable-gpu`, `--disable-gpu-compositing`, `--disable-accelerated-video-decode`, and `--disable-webrtc-hw-decoding` through WebView2 EnvironmentOptions. Setting only the old loader's environment variable did not work on this machine. This switch does not replace the controller, screenshot protocol, task resources, or Pipeline architecture.

On 2026-10-21, a Xeon cloud desktop with 32 logical processors and no discrete GPU completed a roughly 60-minute CPU-only daily queue: eight tasks succeeded and five failed, including an actual Protocol Space battle and reward claim. Failures included account-locked storage, Environment Monitoring localization, and AutoCollect navigation. This is not a full-task pass and does not establish performance on low-end CPUs. Median total CPU utilization was about 8%–16% for light tasks and 41% during battle. Summed process working sets include shared pages and are not private memory.

## Implementation Scope

`agent/cpp-algo/cloud-webview/` reuses the existing `Common/WebView2` and system WebView2 Runtime to host the [official website](https://endfield.hypergryph.com/cloud), without installing another browser. Windows builds generate and install `agent/cloud-webview.exe`. `--launch` starts the window and exits after WebView2 initialization; if the window already exists, it returns immediately.

`CloudWindow` owns the cloud-specific page policy and proportional viewport. The shared window classes provide an initial activation setting that defaults to enabled and a pre-navigation initialization hook; other WebView2 callers retain their existing defaults. MXU still locates `Win32-CloudWeb` by the exact window class and title, but now uses a MaaFramework Custom Controller and RPC over a local pipe identified by the host PID, instead of standard Win32 capture or `Seize` input. Official MXU does not yet provide this integration, so the companion client changes are required. The current name-based entry still needs a generic interface agreed upstream; merging the main repository alone does not make it usable.

The shared `WebView2::resizeToClientRect` also skips minimized and zero-sized updates. In a controlled browser regression on 2026-10-20, the old update path produced CDP `mouseMoved` callbacks roughly one second apart after minimization, taking about 15.1 seconds for 20 moves. Preserving the valid viewport reduced the same case to about 0.33 seconds; every move arrived as a trusted browser event and the list actually scrolled 300 pixels. This isolates and verifies the browser-layer cause. Minimized dragging in the real cloud game must still pass before production integration.

The Windows website provides the PC layout. Resources start with `resource` and only append `resource_cloud_web`. Do not load `resource_adb` or `resource_cloud_adb`. The override layer handles website startup, the title screen without the native exit icon, menu and button color differences, and the Esc menu entry. In-game workflows continue to reuse existing PC Pipelines and Go Service.

This controller supports Windows only. Android continues to use its existing Cloud Endfield client and `CloudADB` implementation, without building the WebView2 host, running web pretasks, or loading cloud web overlay resources.

The cloud floating menu overlaps some game buttons in the top-left corner. The website may ignore keyboard events while the pointer rests over that menu or the network status overlay. Returning to the friend menu, opening the world menu, leaving a space, and leaving the Protocol Space preparation screen first move the pointer to the video center, then recognize the current scene again before sending Esc; the preparation flow also verifies that the original page has closed. Returning from the menu to the world also includes the existing monthly-pass popup handler, covering reward interruptions encountered when testing crosses the daily reset. The network status overlay covers parts of button templates in the bottom-right corner, so teleport and region overview entries recognize the complete button text while retaining map-state checks. The Growth Chamber return button reuses the common `BackButton` to avoid matching the right-side background gradient with the original task template. Protocol Space overrides cover the complete entry text, closing the mechanics notice, and the reward-page 「重新挑战」 and 「离开」 buttons. Reward-button recognition also requires the reward-page title, avoiding reliance on white RGB ranges altered by stream compression. Outpost 「交易」 retains its existing OCR and shifts the click box 60 pixels left into the button, away from the lower-right edge near the network overlay. The original failed click left the trade page open; the shifted click on the same page completed the trade and matched the reward header.

## Build and Debug

Prepare C++ and the WebView2 SDK using the [development environment guide](./tools-and-debug.md), then run:

```powershell
uv run build-and-install --cpp-algo
```

Development builds can also build the `cloud-webview` CMake target separately. Connect Go Service when debugging tasks so that unregistered custom actions or recognitions are not mistaken for compatibility failures. Reload resources after changing overrides; rebuilding the host is unnecessary.

The WebView2 SDK stores sign-in state in the separate `agent/cloud-webview.exe.WebView2/` folder under the working directory. Do not read or commit that folder, screenshots containing UIDs, browser data, or account logs.

Cloud resources move the reset position of `MouseMoveReset` and `IMSA3MouseMoveReset` from the top-left corner to the lower video edge, avoiding activation of the official floating button by pointer resets. Friend-price highlights use color ranges measured from cloud video, retaining the existing regions, connectivity checks, and subsequent scene validation.

Yellow adjustments are limited to the cloud resource layer: shared yellow confirmation buttons use the hue range in `YellowButtonBackground`, while friend-price highlights use a higher blue-channel limit. Existing cloud video samples show different yellow hues and saturations across screens, which does not establish a uniform color shift. Nodes with independent ColorMatch rules require their own scene evidence and positive/negative cases; do not recolor entire frames or widen every yellow threshold globally.

Cloud Web has no local game identity in the registry. `CaptureUid` reuses existing OCR to read the UID from the game image and associates zipline records through the existing salted identity flow. Cloud resources publish that identity after confirming the world scene, covering standalone navigation tasks that bypass menu checks. A cached UID avoids repeated screenshots and OCR. `ZiplineImport` supports the cloud controller, with the region determined by the fixed CN website. Import continues to use the normal WebView2 in `cpp-algo.exe` with a separate browser directory. Cloud input policies and lifecycle actions apply only to `cloud-webview.exe` and must not close the import window or clear its login session.

## Protocol and Validation Limits

Shared frames include protocol version, dimensions, stride, and pixel format. The client validates and copies all pixels and releases the read-only mapping handle before ACK, then encodes PNG outside the transaction. Pipe transactions have a total deadline and support task cancellation; input already sent is never replayed after connection errors. The host permits one run token, verifies ownership using the actual pipe client PID, and can recover ownership after a client exits.

Cloud web AutoCollect and target inventory options are available again, reusing existing PC collection, localization, turning, and pickup flows. The earlier hidden entry resulted from task controller allowlists rather than missing controller capabilities; enabling the entry does not establish acceptance for every route. Isolated browser regressions and historical live runs do not replace a complete daily run on the current version, QQ/WeChat screenshot tests, multiple displays, or low-end CPU validation. Cases depend on the separate image submodule; missing cloud images fail explicitly rather than producing green results by skipping cases.
