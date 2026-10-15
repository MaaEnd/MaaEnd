# 定位节点截图测试

`map_locate.py` 用静态截图验证 `MapLocateAssertLocation` 定位节点：在截图上执行原节点的识别，检查定位结果是否落在节点 `target` 矩形内。

`MapLocateAssertLocation` 由 C++ Agent 提供，`maa-tools test` 不启动 Agent，无法覆盖这类节点，因此本测试使用独立的 Python 执行器，不依赖 `maa-tools`、`loader.mts` 或普通测试 Schema。

当前纳入范围为自动采集已注册路线（Route1–17、CommonRoute1–8）中的定位节点，范围由 `collect_nodes` 决定。

## 工作方式

- 通过 `AgentClient` 启动 `install/agent/cpp-algo` 并加载仓库 `assets/resource`。
- 使用离线截图控制器：任何截图都返回当前样本，所有输入操作都被拒绝，不连接游戏。
- 只执行节点的识别，不执行其 action、next 或路线移动。节点内部的追踪重置与多帧等待保持生产行为。
- 截图按 Win32 小地图 ROI 识别。

## 用例格式

用例文件为 `map_locate_assert_location.json`。它沿用普通测试的 `configs` / `cases` / `image` / `hits` 结构，但特意不使用 `test_*.json` 命名，避免进入 `pnpm test` 的扫描。

```json
{
    "configs": {
        "name": "定位断言（正例）",
        "resource": "官服",
        "controller": "Win32"
    },
    "cases": [
        {
            "image": "自动采集_路线1_定位点.png",
            "hits": ["AutoCollectRoute1AssertLocation"]
        }
    ]
}
```

- `controller` 固定为 `Win32`，`resource` 固定为 `官服`，不展开矩阵。
- `image` 为 `tests/MaaEndTestset/Win32/Official_CN/MapLocateAssertLocation/` 下的 1280×720 PNG 文件名，同一图片只列一次。
- `hits` 为非空节点名数组；同一截图满足多个定位点时可列出多个节点，每个节点独立执行。
- 只支持正例，不使用 `box`（定位节点回填的是地图坐标矩形，不是屏幕框）。

## 补充样本

截图要求：角色位于目标节点的 `target` 矩形内，小地图完整可见，没有被传送横幅等界面遮挡。被遮挡的截图只能依靠遮挡超时后的兜底定位才能通过，验证不到正常路径。

入库前按 `maaend-test-image` 技能对 UID 等信息做脱敏，不要改动小地图区域。

## 运行

```powershell
uv sync --locked
uv run build-and-install --cpp-algo
uv run python tests/MapLocateAssertLocation/map_locate.py
```

也可以用 `pnpm test:map-locate`。运行前确认 `install/agent/cpp-algo` 由当前源码构建，`install/maafw` 与 Agent 版本匹配，`install/resource` 包含定位底图和模型。

可选参数：`--cases <文件>` 指定其它用例文件，`--output <目录>` 指定输出目录。

CI 由 `.github/workflows/test.yml` 中的 `map-locate-test` job 执行：在 Linux x64 上构建当前源码的 `cpp-algo`，再运行本测试。

## 结果

缺图、节点不存在、Agent 不可用、缺少结果或正例未命中时，进程返回非零退出码。输出写入 `output/`（已被 Git 忽略）：

- `report.json`：逐项记录图片、节点、是否命中和错误信息。`sampledNodes` 是有样本的节点，`passedNodes` 是通过的节点，`missingNodes` 是范围内尚无样本的节点及其定位参数。
- `agent.log` 和 MaaFramework 日志：用于排查 Agent 启动、连接和定位失败。

`missingNodes` 非空不会导致失败，它是待补样本的清单。
