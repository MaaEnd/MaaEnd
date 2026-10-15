# Android Build Environment Setup

> **Who is this page for?**
>
> You are building the Android client for the first time, or `uv run tools/build_android_agents.py --check-env` reported a pile of MISS.
>
> This page only covers **installing dependencies and setting environment variables**. For how to build and produce an APK once everything is installed, see [Android/README.md](../../../Android/README.md).

---

## Quick summary

For a first-time environment setup, do three things in order:

1. Install 6 things → [Section 1](#1-install-these-6-things)
2. Set the Android SDK path → [Section 2](#2-set-the-android-sdk-path)
3. Run the environment check once → [Section 3](#3-run-the-environment-check)

**Copy the commands as written and nothing will go wrong.** If you do get an error, find the matching entry in [Section 4](#4-errors-when-installing-dependencies).

After setting things up, **be sure to reopen your terminal**: windows that are already open cannot see the new variables.

---

## 1. Install these 6 things

| What to install | What it is for | How to install |
| --- | --- | --- |
| JDK 17 or later | Compiles the app; Gradle needs it | Windows `winget install Microsoft.OpenJDK.21`; macOS `brew install openjdk@21`, then put `$(brew --prefix openjdk@21)/bin` at the front of `PATH` (brew does not add it) |
| Android SDK | Provides the Android packaging tools | [1.1](#11-install-the-android-sdk) |
| Android NDK | Cross-compiles the C++ agent (cpp-algo); go-service's cgo build also uses its clang | `sdkmanager --install "ndk;29.0.13599879"` |
| CMake ≥ 3.28 | Configures and builds cpp-algo | [1.2](#12-choose-a-cmake) |
| Go 1.25.6+ | Cross-compiles go-service | [Ultra Basic Getting Started Guide](./super-basic-introduction.md) 2.6 |
| uv | Runs the Python scripts in the repository | [Quick Start](./getting-started.md) |

> [!NOTE]
>
> **What are all these, anyway?** The one-sentence version: the JDK is the Java environment that runs Gradle; the Android SDK is the official collection of packaging tools; the NDK is "the compiler that builds C++ for Android"; CMake is the build configuration tool for C++ projects. Just install them all; there is no need to dig deeper.
>
> **You do not need Android Studio**; the command-line tools are enough (you can still install it if you want).

### 1.1 Install the Android SDK

1. Go to the [Android command-line tools download page](https://developer.android.com/studio#command-line-tools-only), and download the zip for your system. The zip contains a single `cmdline-tools` folder; after extracting, rename it to `latest` and put it under `<SDK>/cmdline-tools/` (`<SDK>` is the SDK directory you choose), so that `sdkmanager` ends up in `<SDK>/cmdline-tools/latest/bin`.
2. Add `<SDK>/cmdline-tools/latest/bin` and `<SDK>/platform-tools` to `PATH`.
3. Install the components:

```bash
sdkmanager --licenses
sdkmanager --install "platform-tools" "platforms;android-37.0" "build-tools;37.0.0"
sdkmanager --install "ndk;29.0.13599879"
```

> [!NOTE]
>
> **Do not drop the dot in the version number: it is `platforms;android-37.0`.** Up to API 36, versions were integers (`android-35`, `android-36`); starting with 36.1 they changed to "major.minor" (`android-36.1`, `android-37.0`, `android-37.1`), so guessing "the next integer" gets it wrong. Writing `android-37` reports `Failed to find package`; do not mistake it for a network problem.
>
> The version number comes from `COMPILE_SDK` in `Android/MaaFwApp/build-logic/` (currently 37).

> [!NOTE]
>
> **Keep the NDK version the same as CI**: the `29.0.13599879` above is exactly the one CI uses (see `AGENT_NDK_VERSION` in `.github/workflows/android.yml`). A different version usually builds too, but when something goes wrong locally, switch back to the CI version first to rule it out.

### 1.2 Choose a CMake

There will be **two** CMakes on your system; do not mix them up:

- **The app shell's own**: MaaFwApp hard-codes the SDK's `cmake;3.22.1` in `build.gradle.kts`. **You do not need to deal with this one**; Gradle downloads it automatically on the first build.
- **The one for cross-compiling the agents**: `tools/build_android_agents.py` requires **CMake ≥ 3.28**. **You need to install this one.**

Pick one of the two ways to install it; **the first is recommended**:

| Method | Command | Notes |
| --- | --- | --- |
| **Use the SDK's** (recommended) | `sdkmanager --install "cmake;3.31.6"` | It is ≥ 3.28, which is enough, **and the package ships its own `ninja`, so you do not need to install it separately** |
| Install a system CMake | Windows `winget install Kitware.CMake`; macOS `brew install cmake` | Also works, but you have to make sure Ninja is on `PATH` yourself |

The script first looks through `<SDK>/cmake/*` and then `PATH`, so both of the above are recognized.

### 1.3 Checkpoint

Not sure whether you have everything? **Run the environment check first**; it lists, item by item, what is missing and how to fix it:

```bash
uv run tools/build_android_agents.py --check-env
```

Once you see `OK  environment ready`, you can go to [Android/README.md](../../../Android/README.md) and build.

> [!TIP]
>
> Once the check passes, there is **no need** to double-check by typing `cmake --version` / `ninja --version` by hand. If you took the "use the SDK's CMake" route in [1.2](#12-choose-a-cmake), these two commands may not be on `PATH` at all, and typing them by hand can mislead you into thinking they are not installed.

---

## 2. Set the Android SDK path

**Only the SDK path is required**; setting the other two makes things more reliable:

| Environment variable | Required? | Points to | Notes |
| --- | --- | --- | --- |
| `ANDROID_HOME` or `ANDROID_SDK_ROOT` | **Required** (either one) | Android SDK root directory | The script uses it to find NDK / CMake / Ninja |
| `ANDROID_NDK_ROOT` | Recommended | NDK root directory (e.g. `.../ndk/29.0.13599879`) | If it is not set, the script picks the newest one in `<SDK>/ndk` itself |
| `JAVA_HOME` | Recommended | JDK root directory | If it is not set, the `java` on `PATH` is used; recommended when several JDKs are installed |

> [!WARNING]
>
> **Installing is not enough on its own; the SDK path must be set.** Otherwise the check's Android SDK item reports `ANDROID_HOME / ANDROID_SDK_ROOT is not set`.

---

## 3. Run the environment check

```bash
uv run tools/build_android_agents.py --check-env
```

It only reports: it does not install anything, and it does not scan your disk for dependencies; Go is only checked for presence on `PATH`, not for its version. If everything is green, you can start building; if there is a MISS, fix the items one by one following the hints. Each one states the current problem and how to fix it.

<details>
<summary>What a passing check looks like</summary>

```text
Android build environment check
  Windows AMD64, python 3.14.2

OK  environment ready: JDK 17+, Android SDK, Android NDK, CMake >= 3.28, Ninja, Go, MaaUtils submodule
run: uv run tools/build_android_agents.py
```

The second line is your own system and Python version (on macOS it will be `Darwin arm64`); it **does not need to match the above exactly**.

There is only one criterion: `OK  environment ready` appears. The items listed on that line are the same for everyone.

</details>

---

## 4. Errors when installing dependencies

### `Warning: Failed to find package 'platforms;android-37'`

The package name is missing the decimal part. After API 36, version numbers are `major.minor`, so write `platforms;android-37.0`. See [1.1](#11-install-the-android-sdk).

> [!NOTE]
>
> It is labeled `Warning:` rather than `Error:`, so it is easy to skim past, but the exit code is **1**, and if even one package name in the command is wrong, **the whole command counts as failed** (none of the other packages in that command get installed either).

### `Java version 17 or higher is required`

The Java that `sdkmanager` found is older than 17 (the `java` from `JAVA_HOME` or `PATH` is too old). Install a JDK 17+ and set `JAVA_HOME`, then open a new terminal and try again.

### `CMake was unable to find a build program corresponding to "Ninja Multi-Config"`

Ninja cannot be found during the configure step. Install the SDK's CMake (which ships its own `ninja`) as described in [1.2](#12-choose-a-cmake), and the script will find it automatically.

> [!NOTE]
>
> If the check says Ninja is OK but typing `ninja --version` yourself prints nothing, that is normal: the script uses the copy in `<SDK>/cmake/*/bin`, which is not on `PATH`. Go by the check result. If you really want to add it yourself, **append it to the end** (putting it at the front lets that CMake 3.22.1 shadow the newly installed version).

### `CMake >= 3.28 required, but the newest one found is 3.22.1`

Your system only has an old CMake. See [1.2](#12-choose-a-cmake) for how to install one.

---

## Related

- [Android/README.md](../../../Android/README.md) — how to build, package and release once everything is installed
- [Quick Start](./getting-started.md) — desktop environment setup and Pipeline development
- [Ultra Basic Getting Started Guide](./super-basic-introduction.md) — installing Git, a terminal, Node and Go from scratch
