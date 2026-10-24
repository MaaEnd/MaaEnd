# 用云串流原始帧做识别（CDP 抓帧 + 自定义控制器）

> 结论先行：云串流给的是 **1920×1080**，而识别层现在吃的是「1080→1440 放大→720 缩小」的二次重采样帧。
> 改从 `<video>` 抠原生帧、自己用 **AREA/LANCZOS** 降到 720p，同屏实测锐度 **+28~30%**。
> 缩放算法比来源更重要：同一份原生帧用 **BILINEAR 反而 −30%**。

## 1. 实测：图像是怎么来的

```
云平台串流           1920×1080 @60fps     （video.videoWidth/videoHeight 实测）
  ↓ 浏览器渲染         CSS 2561×1440        ← 因为 sway 输出是 2560×1440，被放大 1.33 倍
  ↓ 合成器             sway HEADLESS-1 2560×1440
  ↓ wlr-screencopy     MaaFw 控制器截图
  ↓ MaaFw 默认缩放      1280×720             ← 识别层真正看到的
```

链路里有两处重采样：浏览器放大（1080→1440）、MaaFw 缩小（1440→720）。

## 2. 实测：为什么必须保持 1280×720 抓帧

扫上游 `resource/pipeline/nodes.json`：

```
节点 5997，带框 650，最大右边界 1287，最大下边界 800
```

650 个上游模板全部在 **1280×720 空间**里抠的。MaaFw 模板匹配不做缩放，
一旦把抓帧改成 1080p，这 650 个框全部失效；而且每次上游更新都会覆盖回来，无法长期维护。

**所以抓帧分辨率被锁在 1280×720，能优化的只有「这 720p 有多干净」。**

## 3. 实测：同屏、静止画面（两次 720p 抓帧平均像素差 0.98）

| 来源 | 720p 锐度（Laplacian 方差） | 相对现状 |
|---|---|---|
| 现有链路（1080→1440→720） | 501.1 | 基准 |
| 原生 1080 + **LANCZOS** | **651.3** | **+30%** |
| 原生 1080 + **AREA/BOX** | **640.7** | **+28%** |
| 原生 1080 + BICUBIC | 534.0 | +7% |
| 原生 1080 + BILINEAR | 353.2 | **−30%** |

原生 1080 与现有链路 720p 之间：PSNR 12.27 dB、平均像素差 44.9（含色彩空间差异）。

推论：
- **不要**让 Chrome 或 MaaFw 用 bilinear 做这次降采样；
- 「把舞台直接做成 720p 让 Chrome 下采样」这条捷径有风险——Chrome 视频缩放是 bilinear 量级，可能比现状更糊。

## 4. 设计：CDP 自定义控制器

`maa.controller.CustomController` 提供全套回调：`connect / screencap / click / swipe /
touch_down / touch_move / touch_up / press_key / key_down / key_up / input_text / scroll / shell`。

```
screencap()  JS 从 <video> 画进 1920×1080 canvas → JPEG(q≈0.92) → Python 解码
             → AREA/LANCZOS 降到 1280×720 → 返回 BGR ndarray
click(x,y)   1280×720 坐标 → CSS 坐标（×(cssW/1280, cssH/720)）
             → CDP Input.dispatchMouseEvent(mousePressed/mouseReleased)
press_key()  CDP Input.dispatchKeyEvent，windowsVirtualKeyCode = 管线里的 Win32 虚拟键码
input_text() CDP Input.insertText
```

收益：
- 识别层拿到的是**流原生像素**，不经浏览器放大、不经合成器；
- 不再依赖 sway（截图与输入都走 CDP），wayland socket / 窗口几何那类问题消失；
- 帧里没有浏览器 UI、没有云平台悬浮球。

## 5. 已知代价与风险

| 风险 | 处理 |
|---|---|
| **DOM 弹窗不在视频里**（`您上次游戏的数据正在火速处理中`、月卡、`点击任意位置继续`若为网页层） | 交给 JS 侧监听并自动关闭（`auto_dismiss_dialog.py`），不让管线靠模板识别 |
| CDP 合成输入是否被云客户端接受 | 先做点击/按键单项验证，再跑整条任务 |
| 抓帧延迟（JPEG 编码+传输约 40–80 ms） | 控制器内做帧缓存（例如 90 ms 内复用上一帧） |
| 帧率/撕裂 | 视频元素是 60fps 连续解码，单帧拷贝无撕裂风险 |

## 6. 验收标准

1. `screencap()` 返回 1280×720，锐度 ≥ 620（对照现有 501）；
2. `click()` 能把「开始游戏」点下去（页面跳转可见）；
3. `press_key()` 能打开游戏内菜单（验证 Win32 虚拟键码映射）；
4. `WebStartGame` 在 CDP 控制器下通过，耗时不超过 sway 方案的 1.5 倍；
5. 全部通过后，homesrv 是否切换另行决定（当前 sway 方案是已验证的生产形态）。

## 7. 验收结果（2026-10-23，WSL 实测）

| 验收项 | 结果 |
|---|---|
| `screencap()` 尺寸/锐度 | 1280×720；锐度 1174（对照现有链路 501，**+134%**；场景不同时 1598~1700） |
| 抓帧耗时 | 后台预抓线程约 190 ms/帧；`screencap()` 命中缓存 **0–7 ms** |
| 鼠标点击 | ✅ 点 1280×720 坐标 (1252,30) → CSS (1669.7,40.0)，Esc 菜单关闭回到大世界 |
| 键盘 | ✅ `key_down/key_up(27)` 打开 Esc 系统菜单 |
| 整条任务 | ✅ `run_daily_linux.py --controller cdp --tasks WebStartGame` → **OK 1.0s**（已在大世界，`WebAlreadyInGame` 一次命中） |

### 直跑踩到的坑

`run_daily_linux.py` 依赖 `resolve_preset.ROOT` 定位 `config/` 与 `interface.json`；
该默认值原先是 Windows 安装目录，直跑时会读到**另一份** interface，
于是自有任务被判「未知任务」。已改为回落到 `MAAEND_DIST` 或 `~/maaend`。

### 使用

```bash
# 走 CDP 原生帧
MAAEND_ROOT=~/maaend python3 run_daily_linux.py --instance hmuddz8 --controller cdp --tasks WebStartGame
# 走原来的合成器截图
... --controller wlroots
```
