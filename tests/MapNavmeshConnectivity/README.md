# 连通性测试

`navmesh_connectivity.py` 验证起点与终点之间能否由寻路规划出路线：按用例文件给出的起点和终点交给 Agent 规划，规划成功即视为连通。它不使用测试截图，也不执行定位，也不读取 Pipeline 节点，与 [定位节点截图测试](../MapLocateAssertLocation/README.md) 完全解耦。

`MapNavmeshQuery` 由 C++ Agent 提供，`maa-tools test` 不启动 Agent，因此本测试使用独立的 Python 执行器。

## 用例格式

用例文件为 `navmesh_connectivity.json`，沿用 `configs` / `cases` 结构，但特意不使用 `test_*.json` 命名，避免进入 `pnpm test` 的扫描。

```json
{
    "configs": {
        "name": "自动采集连通性（正例）"
    },
    "cases": [
        {
            "name": "AutoCollectRoute1AssertLocation",
            "zone_id": "Wuling_Base",
            "start": [951, 1788],
            "goal": [933, 1706]
        }
    ]
}
```

- `name`：用例名，不可重复。当前用例以对应的定位节点命名，便于回溯。
- `zone_id`：起点所在区域，按定位器的区域命名填写（与 `MapLocateAssertLocation` 的 `zone_id` 一致）。
- `start`：起点 `[x, y]`，坐标系与 `zone_id` 一致；分层区起点会由 Agent 先换算到底图坐标。
- `goal`：终点 `[x, y]`，即 `NAVMESH` 的 `target`。
- `goal_tier`（可选）：终点为分层区坐标时填写，对应 `NAVMESH` 的 `target_tier`。
- `goal_deck_y`（可选）：终点需落在指定高度的地面层时填写，对应 `NAVMESH` 的 `target_deck_y`。

每条用例按 `[ZONE, NAVMESH]` 两步路径规划，只判断能否规划成功，不检查路线形状。

## 当前用例

当前用例覆盖自动采集已注册路线（Route1–17、CommonRoute1–8）中，每个 `MapLocateAssertLocation` 定位点到随后首个 `NAVMESH` 目标的一段：

- 起点：定位节点的 `zone_id`，坐标取 `target` 矩形 `[x, y, w, h]` 的中心。
- 终点：定位节点之后第一个 `MapNavigateAction` 的 `path` 中首个 `NAVMESH` 的 `target` 及其 `target_tier` / `target_deck_y`。
- 导航节点 `path` 中没有 `NAVMESH` 的定位点（只有逐点路径）不需要寻路规划，不纳入用例。
- 首个 `NAVMESH` 之前的 `RUN` 或逐点坐标不纳入，直接从起点规划到终点。

用例数据是一次性从 Pipeline 提取后手工维护的。修改相关路线的定位点或 `NAVMESH` 目标时，需同步更新本文件。

## 工作方式

- 通过 `AgentClient` 启动 `install/agent/cpp-algo` 并加载仓库 `assets/resource`。
- 使用离线空控制器：截图返回空白画面，所有输入操作都被拒绝，不连接游戏。
- 每个用例调用一次 `MapNavmeshQuery` 的 `route_preview`，与 `MapNavigateAction` 运行前的路线展开同源。
- `route_preview` 返回 `ok: true` 即判定通过。

## 运行

```powershell
uv sync --locked
uv run build-and-install --cpp-algo
uv run python tests/MapNavmeshConnectivity/navmesh_connectivity.py
```

运行前确认 `install/agent/cpp-algo` 由当前源码构建，`install/maafw` 与 Agent 版本匹配，`install/resource/model/map/navmesh/` 下存在 `base.nav.gz`（或 `base.nav`）。

可选参数：`--cases <文件>` 指定其它用例文件，`--output <目录>` 指定输出目录。

CI 由 `.github/workflows/test.yml` 中 `map-locate-test` job 的 `Run navmesh connectivity tests` 步骤执行，复用该 job 构建的 Agent；定位测试失败时本步骤仍会执行。

## 结果

用例文件格式错误、Agent 不可用、缺少规划结果或任一用例规划失败时，进程返回非零退出码。输出写入 `output/`（已被 Git 忽略）：

- `report.json`：逐项记录用例、实际发送的规划参数、是否通过、错误信息，以及失败时 Agent 返回的 `failure`（失败代码、所在路径下标、分段起止点、断口等）。`failedCases` 列出规划失败的用例。
- `agent.log` 和 MaaFramework 日志：用于排查 Agent 启动、连接和规划失败。
