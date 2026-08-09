# 测试与人工审核

公开测试入口位于 `agent/cpp-algo/source/IconRecognition/test/`。CMake、C++ 测试、`run-tests.ps1`、`run-tests.local.example.psd1` 和 `rois.json` 随 Git 提交；以下内容只用于本机测试并被忽略：

- `input/`：按网格类型分类的测试截图；
- `output/`：标注图、detail JSON 和报告；
- `build/`：CMake 构建目录；
- `run-tests.local.psd1`：可选的本机工具链路径配置。

生产代码和测试都读取 `assets/data/IconRecognition`、`assets/resource/image/IconRecognition` 与 `assets/locales/interface`，不维护测试专用 catalog 或模板副本。

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
}
```

脚本只接受以上两个本地配置字段。显式传入的 `-CMakePath`、`-VsDevShellPath` 优先于本地配置；未配置时继续从 `PATH` 查找 `cmake`，并使用当前 PowerShell 环境。

配置完成后，普通 PowerShell 和 Visual Studio Developer PowerShell 使用同一个入口：

```powershell
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task configure
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task quick
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -All
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -GridType transfer
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -GridType transfer -Image 43.png -Side all
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task manual -Image 43.png
```

无参数、`-Help`、`-h` 会打印完整用法；PowerShell 保留的 `-?` 会显示脚本参数帮助。人工 runner 支持三种选择范围：

- `-All`：遍历所有分类目录；
- `-GridType <type>`：测试某一种网格的全部图片，可与 `-Image` 组合；
- `-Image <basename>`：按完整 basename 精确匹配；未指定网格类型时，同名图片会在所有分类中运行。

`-Side` 只用于 `transfer` 和 `port_storager`，默认是 `full`：

| 值 | 每张图片执行的 ROI |
| --- | --- |
| `full` | 完整大 ROI 一次 |
| `left` | 左侧 ROI 一次 |
| `right` | 右侧 ROI 一次 |
| `split` | 左、右 ROI 各一次 |
| `all` | 完整、左、右 ROI 各一次 |

参数冲突、缺值、未知网格类型或非双侧网格使用 `-Side` 时会打印原因和用法，并返回非零退出码。

## 查看结果

每次人工运行创建独立的 `output/<时间戳>-<选择范围>/`，不会覆盖之前的审核结果：

- `annotated/<grid-type>-<roi-name>-<文件名>.png`：完整原图上的 ROI、cell 和 item 框；下方审核栏列出编号、发布原图标、中文名、item ID、分数与网格坐标。
- `detail/<grid-type>-<roi-name>-<文件名>.json`：公开结果加内部 diagnostics。
- `report.json`：本次 case 数、失败数，以及每个 case 的图片相对路径、`grid_type`、`roi_name`、ROI、命中数和输出路径。

人工审核时依次检查 ROI 是否覆盖正确区域、编号 cell 是否对应审核栏原图标与中文名、item 框是否贴合、分数是否合理，以及红色拒识格是否符合预期。
