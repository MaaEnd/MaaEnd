# Android 构建环境准备

> **这页写给谁？**
>
> 你第一次要编 Android 客户端，或者 `uv run tools/build_android_agents.py --check-env` 报了一堆 MISS。
>
> 这页只讲**装依赖、配环境变量**。装完之后怎么构建、怎么出 APK，看 [Android/README.md](../../../Android/README.md)。

---

## 先看结论

第一次配环境，按顺序做三件事：

1. 装 6 样东西 → [第 1 节](#1-装这-6-样)
2. 配好 Android SDK 路径 → [第 2 节](#2-配-android-sdk-路径)
3. 跑一次体检 → [第 3 节](#3-跑一次体检)

**照着命令抄就不会出错。** 万一报错，去 [第 4 节](#4-装依赖时的报错) 对号入座。

配完**务必重开终端**，已经开着的窗口读不到新变量。

---

## 1. 装这 6 样

| 要装的东西 | 干什么用的 | 怎么装 |
| ---------------- | -------------------------------- | ----------------------------------------------------------------------------------- |
| JDK 17 及以上 | 编译 App，Gradle 要用 | Windows `winget install Microsoft.OpenJDK.21`；macOS `brew install openjdk` |
| Android SDK | 提供各种 Android 打包工具 | [1.1](#11-装-android-sdk) |
| Android NDK | 交叉编译 C++ agent（cpp-algo） | `sdkmanager --install "ndk;29.0.13599879"` |
| CMake ≥ 3.28 | 配置并构建 cpp-algo | [1.2](#12-选一份-cmake) |
| Go 1.25.6+ | 交叉编译 go-service | [超基础入门](./super-basic-introduction.md) 2.6 |
| uv | 跑仓库里的 Python 脚本 | [快速开始](./getting-started.md) |

> [!NOTE]
>
> **这些到底是什么？** 一句话版：JDK 是跑 Gradle 的 Java 环境；Android SDK 是官方的打包工具集合；NDK 是"给 Android 编 C++ 的编译器"；CMake 是 C++ 项目的构建配置工具。装齐就行，不用深入。
>
> **Android Studio 不需要装**，命令行工具就够（想装也行）。

### 1.1 装 Android SDK

1. 去 [Android 命令行工具下载页](https://developer.android.com/studio#command-line-tools-only) 下载对应系统的 zip，解压到你想放的位置，例如 `<SDK>/cmdline-tools/latest`。
2. 把 `<SDK>/cmdline-tools/latest/bin` 和 `<SDK>/platform-tools` 加进 `PATH`。
3. 装组件：

```bash
sdkmanager --licenses
sdkmanager --install "platform-tools" "platforms;android-37.0" "build-tools;37.0.0"
sdkmanager --install "ndk;29.0.13599879"
```

> [!NOTE]
>
> **版本号里的那个点不能省：是 `platforms;android-37.0`。** API 36 以前是整数版本（`android-35`、`android-36`），从 36.1 起改成"主版本.次版本"（`android-36.1`、`android-37.0`、`android-37.1`），所以按"下一个整数"猜会猜错。写成 `android-37` 报的是 `Failed to find package`，别误判成网络问题。
>
> 版本号来自 `Android/MaaFwApp/build-logic/` 里的 `COMPILE_SDK`（当前 37）。

> [!NOTE]
>
> **NDK 版本跟 CI 保持一致**：上面写的 `29.0.13599879` 就是 CI 用的那个（见 `.github/workflows/android.yml` 的 `AGENT_NDK_VERSION`）。版本不同一般也能编过，但本地出问题时，先换回 CI 的版本排除干扰。

### 1.2 选一份 CMake

系统里会存在**两个** CMake，别搞混：

- **外壳自己的**：MaaFwApp 在 `build.gradle.kts` 里写死了 SDK 的 `cmake;3.22.1`。**这个你不用管**，Gradle 首次构建时自动下载。
- **agent 交叉编译用的**：`tools/build_android_agents.py` 要求 **CMake ≥ 3.28**。**这个要你装**。

装法二选一，**推荐第一种**：

| 装法 | 命令 | 说明 |
| ------------------ | ------------------------------------------- | ---------------------------------------------------- |
| **用 SDK 的**（推荐） | `sdkmanager --install "cmake;3.31.6"` | 它 ≥ 3.28 够用，**而且包里自带 `ninja`，不用再单独装** |
| 装系统 CMake | Windows `winget install Kitware.CMake`；macOS `brew install cmake` | 也行，但 Ninja 要自己保证在 `PATH` 上 |

脚本会先翻 `<SDK>/cmake/*`，再找 `PATH`，所以上面两种都认。

### 1.3 检查点

不确定齐没齐？**先跑体检**，它会逐项列出缺什么、怎么装：

```bash
uv run tools/build_android_agents.py --check-env
```

看到 `OK  environment ready` 就可以去 [Android/README.md](../../../Android/README.md) 构建了。

> [!TIP]
>
> 体检过了就**不用**再手动敲 `cmake --version` / `ninja --version` 复核。如果你走的是 [1.2](#12-选一份-cmake) 里"用 SDK 的 CMake"那条路，这两个命令本来就可能不在 `PATH` 里，手动敲反而会误判成没装。

---

## 2. 配 Android SDK 路径

**只有 SDK 路径是必须的**，另两个设了更稳：

| 环境变量 | 必需？ | 指向 | 说明 |
| ---------------------------------------- | ---------------- | --------------------------------------------- | -------------------------------------------------- |
| `ANDROID_HOME` 或 `ANDROID_SDK_ROOT` | **必需**（二选一） | Android SDK 根目录 | 脚本靠它找 NDK / CMake / Ninja |
| `ANDROID_NDK_ROOT` | 推荐 | NDK 根目录（例如 `.../ndk/29.0.13599879`） | 没设时脚本自己挑 `<SDK>/ndk` 里最新的 |
| `JAVA_HOME` | 推荐 | JDK 根目录 | 没设时用 `PATH` 上的 `java`；装了多个 JDK 时建议设 |

> [!WARNING]
>
> **光装好还不够，SDK 路径必须配上。** 不配的话体检第一项就会报 `ANDROID_HOME / ANDROID_SDK_ROOT is not set`。

---

## 3. 跑一次体检

```bash
uv run tools/build_android_agents.py --check-env
```

它只报告、不安装，也不会去扫描磁盘找依赖。全绿就可以开始构建；有 MISS 就照着提示逐条修，每条都写了该设哪个环境变量、该装什么。

<details>
<summary>体检过了应该长什么样</summary>

```text
Android build environment check
  Windows AMD64, python 3.14.2

OK  environment ready: JDK 17+, Android SDK, Android NDK, CMake >= 3.28, Ninja, Go, MaaUtils submodule
```

第二行是你自己的系统和 Python 版本（macOS 上会是 `Darwin arm64`），**不用和上面完全一致**。

判断标准只有一条：出现 `OK  environment ready`。这一行里列的项目对所有人都一样。

</details>

---

## 4. 装依赖时的报错

### `Warning: Failed to find package 'platforms;android-37'`

包名少了小数位。API 36 以后版本号是 `主版本.次版本`，写 `platforms;android-37.0`，见 [1.1](#11-装-android-sdk)。

> [!NOTE]
>
> 它被标成 `Warning:` 而不是 `Error:`，很容易一眼扫过去，但退出码是 **1**，而且只要命令里有一个包名写错，**整条命令都算失败**（其它包即使装好了也返回 1）。

### `Java version 17 or higher is required`

`sdkmanager` 没找到 JDK。装一个 JDK 17+ 并设置 `JAVA_HOME`，然后重开一个终端再试。

### `CMake was unable to find a build program corresponding to "Ninja Multi-Config"`

配置阶段找不到 Ninja。按 [1.2](#12-选一份-cmake) 装 SDK 的那份 CMake（自带 `ninja`）即可，脚本会自动找到它。

> [!NOTE]
>
> 体检说 Ninja OK、你手敲 `ninja --version` 却没输出，是正常的——脚本找的是 `<SDK>/cmake/*/bin` 里那份，不在 `PATH` 上，以体检结果为准。真要自己加就**追加到末尾**（放前面会让那份 CMake 3.22.1 遮蔽新装的版本）。

### `CMake >= 3.28 required, but the newest one found is 3.22.1`

系统里只有旧版 CMake，装法见 [1.2](#12-选一份-cmake)。

---

## 相关

- [Android/README.md](../../../Android/README.md) —— 装完之后怎么构建、出包、发版
- [快速开始](./getting-started.md) —— 桌面端环境准备与 Pipeline 开发
- [超基础入门](./super-basic-introduction.md) —— Git、终端、Node、Go 从零装起
