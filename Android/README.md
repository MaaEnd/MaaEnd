# Android 客户端

外壳是 [MaaFwApp](https://github.com/Aliothmoon/MaaFwApp) 子模块。资源和两个编译型 agent 用本仓库的树。

MaaFwApp 只认 `agent.sourceDir/<abi>/jniLibs/lib*.so`，构建期由 `syncAgentJniLibs` 铺进 APK 的 `lib/<abi>/`。框架 `.so` 不走这条，由外壳自己的 `setup_maa_framework.py` 铺。

## 首次

```bash
git submodule update --init --recursive
python tools/build_android_agents.py
python Android/MaaFwApp/scripts/setup_maa_framework.py --abi arm64-v8a --tag v5.14.0
```

需要 x86_64（模拟器）或 universal 包时，两边都带上对应 ABI：

```bash
python tools/build_android_agents.py --abi arm64-v8a --abi x86_64
python Android/MaaFwApp/scripts/setup_maa_framework.py --abi all --tag v5.14.0
```

并把 `local.properties` 的 `build.debugAbi` / `build.releaseAbi` 设成 `arm64-v8a,x86_64`。链接期 SDK 按 ABI 放在 `deps-android/<abi>`。

在 `Android/MaaFwApp/local.properties` 里写（不进 git）：

```properties
sdk.dir=<Android SDK>
pi.profile=../profile.yaml
build.debugAbi=arm64-v8a
```

Windows 上 NDK 认 `ANDROID_NDK_ROOT`（或 `ANDROID_HOME/ndk` 里最新一份）。CMake 要 ≥ 3.28，Android SDK 自带的 3.31+ 即可。Go 交叉编必须 `CGO_ENABLED=1`（`purego` 在 Android 上要 cgo 才能 `dlopen`）。

## 出包

```bash
# 改了 go-service / cpp-algo
python tools/build_android_agents.py

# 已连接设备
./Android/MaaFwApp/gradlew -p Android/MaaFwApp :app:installDebug
```

只改 `assets/` 里的任务 / 图，重新 `installDebug` 即可。

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
