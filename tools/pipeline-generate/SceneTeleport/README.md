# 传送锚点

按 zmdmap 的营地数据（`tools/zmdmap/data/teleport_anchors.json`）批量生成大地图上普通传送锚点的节点：

| 文件 | 内容 |
| --- | --- |
| `assets/resource/pipeline/SceneManager/SceneTeleportAnchor.json` | 在大地图上确认锚点并点击的 `MapFind` 节点 |
| `assets/resource/pipeline/Interface/SceneTeleportAnchor.json` | 对外入口 `SceneEnterWorld<大区><子区域><地名>` |

两份文件每次整份重写，不要手改。基建传送点等其它图标的节点仍手写在 `SceneTeleport<大区>.json` 与 `Interface/Scene<大区>.json`。

## 运行方式

```bash
# 在仓库根目录运行
pnpm generate:SceneTeleport
```

依次执行：只取营地数据（`fetch:zmdmap --only teleport_anchors`）→ `sync-anchors.mjs` 把新出现的营地追加进 `anchors.json` → 渲染上面两份 Pipeline。

## `anchors.json`

- `regions`：大区 → 底图（`map`）、`MapFind` 用的区域名（`zone`）、desc 里的大区名（`name`）。
- `levels`：关卡 → 所属大区（`region`）、子区域名（`sub_region`，拼进节点名）、`anchors`（地名 → 营地 ID）。
  地名就是入口节点名末尾那段，其它任务按名字引用入口，已登记的地名不要改。新营地由同步脚本按所在区域的英文名起名，同一子区域里重名的按相对方位加 `North` / `South` / `East` / `West`，仍撞名再接数字；登记后游戏改译名也不跟着变。
- `manual`：仍由手写节点处理的营地（四号谷地的模板匹配节点），只占名字、不生成。

## desc

子区域名取关卡名；区域名取营地所在的有名字的地下层，否则取同关卡地图上离营地最近的地名。同一关卡里区域名相同的几个营地，按各自相对它们中心的方位加「东侧 / 西侧 / 南侧 / 北侧」区分。

## 新增子区域

游戏开放新的子区域时，同步脚本会提示该关卡的营地没有登记子区域。先补齐这片子区域的地图节点：

1. `Interface/Scene<大区>.json`：`SceneEnterMap<大区><子区域>`；
2. `SceneManager/Scene<大区>.json`：切到该子区域地图的节点与 `__ScenePrivateMap<大区><子区域>EnterWorldAnchorWithPick`；
3. `Interface/InScene/Region.json`：`InMap<大区><子区域>`。

再在 `levels` 里登记该关卡，重新运行 `pnpm generate:SceneTeleport`。
