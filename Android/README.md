# Android 客户端

> **先装依赖：** [Android 构建环境准备](../docs/zh_cn/developers/android-build-env.md)——那页讲要装什么、配什么、**哪些不用管，哪些会报错**。
>
> 配好了再回来看这页构建。不确定齐没齐，跑 `uv run tools/build_android_agents.py --check-env`。

外壳是 [MaaFwApp](https://github.com/Aliothmoon/MaaFwApp) 子模块。资源和两个编译型 agent 用本仓库的树。

MaaFwApp 只认 `agent.sourceDir/<abi>/jniLibs/lib*.so`，构建期由 `syncAgentJniLibs` 铺进 APK 的 `lib/<abi>/`。框架 `.so` 不走这条，由外壳自己的 `setup_maa_framework.py` 铺。

## 首次

```bash
git submodule update --init --recursive
uv run tools/build_android_agents.py
uv run Android/MaaFwApp/scripts/setup_maa_framework.py --abi arm64-v8a --tag v5.14.0
```

> [!IMPORTANT]
>
> 两边的 MaaFramework 版本要一致：第一个脚本默认用 `v5.14.0`，第二个用 `--tag` 指定同一个值。写 `5.14.0`（不带 `v`）会 404。

需要 x86_64（模拟器）或 universal 包时，两边都带上对应 ABI：

```bash
uv run tools/build_android_agents.py --abi arm64-v8a --abi x86_64
uv run Android/MaaFwApp/scripts/setup_maa_framework.py --abi all --tag v5.14.0
```

并把 `local.properties` 的 `build.debugAbi` / `build.releaseAbi` 设成 `arm64-v8a,x86_64`。链接期 SDK 按 ABI 放在 `deps-android/<abi>`。

在 `Android/MaaFwApp/local.properties` 里写（不进 git）：

```properties
sdk.dir=<Android SDK>
pi.profile=../profile.yaml
build.debugAbi=arm64-v8a
build.releaseAbi=arm64-v8a
```

## 出包

```bash
# 改了 go-service / cpp-algo
uv run tools/build_android_agents.py

# 已连接设备
./Android/MaaFwApp/gradlew -p Android/MaaFwApp :app:installDebug
```

只改 `assets/` 里的任务 / 图，重新 `installDebug` 即可。

产物位置：

| 产物 | 路径 |
| --------------------------------- | ------------------------------------------------------------ |
| 两个 agent | `Android/agent-dist/<abi>/jniLibs/lib{cpp-algo,go-service}.so` |
| 铺进外壳的框架 `.so` | `Android/MaaFwApp/app/src/main/jniLibs/<abi>/` |
| APK | `Android/MaaFwApp/app/build/outputs/apk/debug/app-debug.apk` |

都是构建产物，**不进 git**，出问题可以放心删掉重来。

升外壳：

```bash
git -C Android/MaaFwApp fetch
git -C Android/MaaFwApp checkout origin/main
git add Android/MaaFwApp
```

## CI

`.github/workflows/android.yml`：agent 按 ABI（arm64-v8a / x86_64）分别交叉编译，再由 MaaFwApp 出包。MaaFramework 版本与 `install.yml` 一致取最新 release，手动运行可用 `maafw_version` 覆盖。

| 触发 | 产物 |
| --- | --- |
| push / PR（`Android/`、`agent/`、构建脚本变更） | `MaaEnd-android-arm64-v8a-<tag>-debug.apk` |
| 手动运行选 `assemble=release` | `MaaEnd-android-universal-<tag>.apk`、`MaaEnd-android-arm64-v8a-<tag>.apk`、`MaaEnd-android-x86_64-<tag>.apk` |
| `v*` tag（由 `install.yml` 调用） | 同上，并随桌面包一起上传到 Release |

上面两种只上传为 Actions artifact；正式版的 APK 进 Release 后，由 `mirrorchyan_release.yml` 上传到 Mirror酱 的 `MaaEnd_exec`（os `android`，arm64-v8a / x86_64 各一个架构，universal 不带架构）。

release 包签名需要仓库 Secrets：`KEYSTORE_BASE64`、`KEYSTORE_PASSWORD`、`KEY_ALIAS`、`KEY_PASSWORD`；未配置时产出未签名包。
