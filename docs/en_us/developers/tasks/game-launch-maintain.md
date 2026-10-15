# GameLaunch Official Launcher Pretask

This document describes the Windows CN official launcher pretask and its maintenance boundaries.  
Updated: 2026-10-15.

## Configuration and Execution

In “Launch Game via Official Launcher (CN)”, enter the absolute path to the Hypergryph `Launcher.exe`, or leave it empty to skip. The game must be installed and launcher login and updates must be complete. This entry does not support the global or other channel launchers.

Before connecting the `Win32-Front` controller, the Client executes `agent/go-service --pretask GameLaunch` with the input values appended as the final argument:

```json
{"GameLaunchLauncher":{"Path":"D:\\Hypergryph Launcher\\Launcher.exe"}}
```

`GameSetting` runs before `GameLaunch` so game settings can be applied before startup. An existing `UnityWndClass` / `Endfield` window is reused. A “游戏启动失败” window reports failure, rather than treating the blank game window as success.

## Recognition and Process Lifetime

Go starts `Launcher.exe --game=endfield --reason=4`, discovers windows, and waits for the game window. `OfficialLauncher/StartGame.json` uses MaaFramework OCR to recognize the full text “开始游戏” and clicks it, then checks the button again after 300ms: continue if it shows “游戏中”, or click a second time only if it still shows “开始游戏”. After the second click, recognition must confirm “游戏中” before Go checks for the game window. Login and scene recognition after game connection remain the responsibility of the existing tasks.

The recognition ROI is `[900, 540, 360, 160]` at the 1280×720 baseline, taken from the actual button at the bottom right of the CN launcher. Obtain a fresh launcher screenshot before changing it. On CN launcher 1.6.0, a single click did not start the game, while two clicks separated by 300ms succeeded. This observed interval is retained, with fresh recognition before the second action. When login, updates, or a blocking dialog prevent startup, the pretask fails for the user to resolve without bypassing prompts or looping retries. Native recognition and action logs are saved under `debug/game-launch/`.

The launcher creates and owns the game process and must remain running. The code only releases handles owned by this process. It does not terminate the launcher, assign the game's parent process, or modify game files or account caches. External multi-account tools should switch accounts after the game exits, then execute this pretask again.

## Validation

```bash
cd agent/go-service
go test ./pretask/...
```

Check that input parsing follows the PI `input` object format and that a game launch failure window takes priority over a normal game window. After native integration changes, run `uv run build-and-install` and check cold startup, an already running game, and pending launcher updates on the Windows CN official launcher. Explicitly list untested scenarios in the PR.
