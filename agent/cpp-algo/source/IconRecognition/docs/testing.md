# 测试与人工审核

公开测试入口位于 `agent/cpp-algo/source/IconRecognition/test/`。CMake、C++ 测试和 `run-tests.ps1` 可以提交；以下内容只用于本机人工测试并被 Git 忽略：

- `input/`：测试截图和本地人工测试清单；
- `output/`：标注图、detail JSON 和报告；
- `build/`：CMake 构建目录；
- `run-local.ps1`：可选的本机环境入口。

生产代码和测试都读取 `assets/data/IconRecognition`、`assets/resource/image/IconRecognition` 与 `assets/locales/interface`，不维护测试专用 catalog 或模板副本。

## 准备图片

1. 把 1280x720 测试截图放入 `agent/cpp-algo/source/IconRecognition/test/input/`。
2. 建议截图前先将鼠标移动到不会遮挡物品网格的位置（例如左上角），再等待目标区域画面稳定。
3. 在同目录的本地 `all-test-images.json` 中按截图文件名配置 `grid_type`、Maa `[x,y,width,height]` 和必要的 `item_ids` / `item_filters`。单 ROI 用例使用 `grid_type: "single_roi"`。

## 运行命令

Visual Studio Developer PowerShell 中可直接运行：

```powershell
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task configure
./agent/cpp-algo/source/IconRecognition/test/run-tests.ps1 -Task quick
```

人工测试由被 Git 忽略的 `run-local.ps1` 读取本地清单，并按完整文件名筛选：

```powershell
./agent/cpp-algo/source/IconRecognition/test/run-local.ps1 -Image 63.png
./agent/cpp-algo/source/IconRecognition/test/run-local.ps1 -All
```

本地脚本默认要求 `-Image`，只有明确传 `-All` 才运行全部已配置截图，避免误触长时间测试。同一文件名配置了多个 ROI 时会依次运行这些配置。

## 查看结果

- `output/annotated/<文件名>-<grid-type>.png`：完整原图上的 ROI、cell 和 item 框；下方审核栏列出编号、发布原图标、中文名、item ID、分数与网格坐标。
- `output/detail/<文件名>-<grid-type>.json`：公开结果和内部诊断。
- `output/report.json`：本次运行的图片、`grid_type`、命中数、输出路径和失败计数。

人工审核时依次检查 ROI 是否覆盖正确区域、编号 cell 是否对应审核栏原图标与中文名、item 框是否贴合、分数是否合理，以及红色拒识格是否符合预期。
