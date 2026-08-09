# 测试与人工审核

公开测试入口位于 `agent/cpp-algo/source/IconRecognition/test/`。CMake、C++ 测试、`run-tests.ps1`、`run-tests.local.example.psd1` 和 `rois.json` 随 Git 提交；以下内容只用于本机测试并被忽略：

- `input/`：按网格类型分类的测试截图；
- `output/`：标注图、detail JSON 和报告；
- `build/`：CMake 构建目录；
- `run-tests.local.psd1`：可选的本机工具链路径配置。

生产代码和测试都读取 `assets/data/IconRecognition`、`assets/resource/image/IconRecognition` 与 `assets/locales/interface`，不维护测试专用 catalog 或模板副本。`quick` 使用仓库内已跟踪的铜矿模板合成 single ROI 输入，不读取被忽略的 `input/`；`input/` 仅供人工 runner 和显式截图回归使用。

## 准备图片

1. 把 1280x720 测试截图放入对应网格目录：

```text
input/
├── trade/*.png
├── transfer/*.png
├── port_storager/*.png
├── valuables/*.png
├── shipment/*.png
├── credit_trade/*.png
└── single_roi/
    └── 1177-450-54/*.png
```

1. 建议截图前先将鼠标移动到不会遮挡物品网格的位置（例如左上角），再等待目标区域画面稳定。
2. 常规网格从 `rois.json` 自动读取 ROI，不需要逐图配置。`single_roi/<x>-<y>-<size>/` 用目录名描述正方形 ROI，例如 `1177-450-54` 会解析为 `[1177,450,54,54]`。

## 运行命令

普通 PowerShell 可将 `run-tests.local.example.psd1` 复制为被忽略的 `run-tests.local.psd1`，并填写本机工具链路径：

```powershell
@{
    CMakePath      = "C:/path/to/cmake.exe"
    VsDevShellPath = "C:/path/to/Launch-VsDevShell.ps1"
    Jobs           = 16
}
```

脚本只接受以上三个本地配置字段。显式传入的 `-CMakePath`、`-VsDevShellPath`、`-Jobs` 优先于本地配置；未配置工具路径时继续从 `PATH` 查找 `cmake`，并使用当前 PowerShell 环境。

配置完成后，普通 PowerShell 和 Visual Studio Developer PowerShell 使用同一个入口：

```powershell
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task configure
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task quick
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -All
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -GridType transfer
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -GridType transfer -Image 43.png -Side all
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -GridType transfer -Side all -Jobs 16
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -Image 43.png
```

`quick` 是干净 checkout 可运行的快速门禁，覆盖类型、参数契约、single ROI、MaaFramework 包装、算法小测试和 debug capture。整图识别及性能回归必须先准备 `input/`，再使用 `-Task manual` 显式运行，不会被 quick 静默跳过。

无参数、`-Help`、`-h` 会打印完整用法；PowerShell 保留的 `-?` 会显示脚本参数帮助。人工 runner 支持三种选择范围：

- `-All`：遍历所有分类目录；
- `-GridType <type>`：测试某一种网格的全部图片，可与 `-Image` 组合；
- `-Image <basename>`：按完整 basename 精确匹配；未指定网格类型时，同名图片会在所有分类中运行。

`-Side` 只用于 `transfer` 和 `port_storager`，默认是 `full`：

| 值 | 每张图片执行的 ROI |
| ------- | ----------------------- |
| `full` | 完整大 ROI 一次 |
| `left` | 左侧 ROI 一次 |
| `right` | 右侧 ROI 一次 |
| `split` | 左、右 ROI 各一次 |
| `all` | 完整、左、右 ROI 各一次 |

参数冲突、缺值、未知网格类型或非双侧网格使用 `-Side` 时会打印原因和用法，并返回非零退出码。

`-Jobs` 只并行不同测试 case，不改变单张图片内部的生产识别算法。未显式传入时读取本机配置，仍未配置则为 1；C++ runner 直接调用时还支持 `--jobs auto`，按物理核心数选择并最多使用 16 个 worker。多 worker 模式把 OpenCV 内部线程限制为 1，避免 worker 数与 OpenCV 线程数相乘。

每个 worker 只写自己的 annotated/detail 文件；主线程按 case 发现顺序生成控制台输出和 `report.json`。报告额外记录 `jobs`、`opencv_threads`、`elapsed_seconds` 和 `cases_per_second`。

需要分析性能时，在 PowerShell 命令中加入通用参数 `-Debug`；直接运行 C++ runner 时使用 `--debug`。debug 模式会在控制台打印启动和单 case 耗时，在 detail 的 `diagnostics.performance` 中记录网格检测、模板选择、候选排名、纹理、稀有度、结果组装，以及 matcher 内部的画布准备、相位变换、模板匹配、极值归约、Lab 转换和颜色距离；`report.json` 还会记录 `startup_performance` 与各 case 的 `runner_performance`。正常模式不采集这些计时。示例：

```powershell
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -GridType transfer -Image 37.png -Side full -Jobs 1 -Debug
```

生产自定义识别参数中的 `debug: true` 同样会采集 `diagnostics.performance`，并随现有 debug capture 写入 detail JSON。细粒度计时会引入少量观测开销，比较绝对耗时时应固定图片、ROI、构建配置和 worker 数，并至少重复三次。

`diagnostics.performance.ranking` 中与 rarity 候选缩减相关的计数为：

- `rarity_prefiltered_cells`：实际启用同 rarity 首轮的 cell 数；
- `rarity_fallback_cells`：首轮未达到阈值并执行剩余候选的 cell 数；
- `rarity_preferred_candidates`：首轮实际评分的同 rarity 模板总数；
- `rarity_remaining_candidates`：回退轮实际评分的其余模板总数。

`baseline_candidates` 是两轮实际基础评分总数。回退时首轮和剩余候选互斥，因此单个 cell 的两轮总数不会超过原候选数量；`matcher.score_calls` 还会额外包含必要的亚像素相位评分。

## 查看结果

每次人工运行创建独立的 `output/<时间戳>-<选择范围>/`，不会覆盖之前的审核结果：

- `annotated/<grid-type>-<roi-name>-<文件名>.png`：完整原图上的 ROI、cell 和 item 框；下方审核栏列出编号、发布原图标、中文名、item ID、分数与网格坐标。
- `detail/<grid-type>-<roi-name>-<文件名>.json`：公开结果加内部 diagnostics。
- `report.json`：本次 case 数、失败数，以及每个 case 的图片相对路径、`grid_type`、`roi_name`、ROI、命中数和输出路径。

人工审核时依次检查 ROI 是否覆盖正确区域、编号 cell 是否对应审核栏原图标与中文名、item 框是否贴合、分数是否合理，以及红色拒识格是否符合预期。

## 双侧网格回归门槛

可信色带和规则晶格的定向测试至少覆盖：

- 六档 rarity 覆盖向量，以及灰色/黄色同色背景与真实窄条的区别；
- 同一行混合 rarity 共同支持晶格，不要求整行同色；
- 浮点 pitch 的整数投影无累积误差，递增可变 pitch 序列被拒绝；
- `transfer` 1、25、100、106、108 的相位和稀疏边界；
- `transfer` 4、41、53 的 full cell 集合等于独立 left + right；
- `port_storager/1` 左右两侧不同 origin。

截图编号只用于测试入口，不得进入生产判断。全量审核使用 transfer 112 张图片的 full/left/right 共 336 个 case，并覆盖 port_storager 的全部可用 ROI。正确 match 总数不能整体下降，新增远端整片错位立即视为失败；结构和色带都不足时，明确失败优于输出低置信网格。

人工 detail 的 `diagnostics.grids[]` 应同时检查 pitch 位于 68–70px、最大残差不超过 2.25px、可信 rarity 计数、fallback 原因，以及 full/split 的 origin、pitch、行列数是否一致。
