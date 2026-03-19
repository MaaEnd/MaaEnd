# EssenceFilter matchapi

提供一个纯 Go 的“OCR -> 基质技能 -> 匹配武器/技能”的能力，完全不依赖 `maa`、不包含点击/滑动等动作逻辑。

外部调用者只需要把你自己的 OCR 结果（技能文本 + 等级）丢进来，再传入匹配选项，最终得到结构化的匹配结果（武器名/技能、命中类型、是否应该锁定/废弃等）。

## 包路径

```go
import "github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
```

## 数据加载（默认）

默认会从仓库的 `assets/data/EssenceFilter/*` 加载数据（`matcher_config.json`、`skill_pools.json`、`weapons_output.json`、`locations.json`）。

如果你的运行环境无法自动定位到 `assets/data/EssenceFilter`，可以设置环境变量：

`MAAEND_ESSENCEFILTER_DATA_DIR=/path/to/assets/data/EssenceFilter`

## 最简单用法：只调用匹配

```go
engine, err := matchapi.NewDefaultEngine()
if err != nil {
    // 例如无法定位 assets/data/EssenceFilter
    panic(err)
}

ocr := matchapi.OCRInput{
    Skills: [3]string{"力量", "攻击", "寒冷"}, // 这三条不要求严格按 slot1/slot2/slot3 顺序；引擎会基于 pool 自动重排（若能唯一推断）
    Levels: [3]int{1, 1, 3},                     // 对应等级（1..6）
}

opts := matchapi.EssenceFilterOptions{
    // exact 精确匹配只在你选择了稀有度时才启用
    Rarity6Weapon: true,

    KeepFuturePromising: false,
    KeepSlot3Level3Practical: false,

    DiscardUnmatched: false,
}

res, err := engine.MatchOCR(ocr, opts)
if err != nil {
    panic(err)
}

// res.Kind: MatchExact / MatchFuturePromising / MatchSlot3Level3Practical / MatchNone
// res.Reason: 各 Kind 均有人类可读文案（见下表）
// res.ShouldLock / res.ShouldDiscard: 供你决定上锁/废弃策略
// res.Weapons: exact 命中时返回候选武器列表（可能多把）
// res.SkillIDs / res.SkillsChinese: 命中的技能ID与中文名
```

## 规则开关怎么对应你描述的需求？

1. “总数大于 x（6）”
  - 使用 `KeepFuturePromising=true`
  - 设置 `FuturePromisingMinTotal=x`（例如 6）
  - `LockFuturePromising` 决定是否命中后应该锁定
2. “slot3 大于（3）”
  - 使用 `KeepSlot3Level3Practical=true`
  - 设置 `Slot3MinLevel=3`
  - 注意：slot3 可能出现在 OCR 的任意位置（slot1/2/3 文本里可能混入），引擎会自动判定 slot3 池命中的那条
  - `LockSlot3Practical` 决定是否命中后应该锁定
3. 未命中怎么处理
  - `DiscardUnmatched=true` -> `res.ShouldDiscard=true`
  - `DiscardUnmatched=false` -> 不废弃，`res.ShouldDiscard=false`

## 输出结构（MatchResult）

公共字段：

- `Kind`：命中类型（见下表）
- `Weapons`：精确匹配时为候选武器列表（可能多把同名组合）；扩展规则下多为空或少量关联武器
- `SkillIDs` / `SkillsChinese`：槽位技能 ID 与中文名（exact 为池内规范名；扩展规则可能为 OCR 原文）
- `ShouldLock` / `ShouldDiscard`：由引擎根据规则与选项给出的操作建议；实际是否锁定/废弃由调用方决定
- `Reason`：**各 `Kind` 均会填充**，便于日志与 UI 统一展示

### 按 `Kind` 的典型输出


| `Kind`                      | `Weapons` | `SkillIDs` / `SkillsChinese`             | `ShouldLock`          | `ShouldDiscard`    | `Reason` 格式                                |
| --------------------------- | --------- | ---------------------------------------- | --------------------- | ------------------ | ------------------------------------------ |
| `MatchExact`                | 非空（可能多把）  | 长度 3，对应目标组合                              | `true`                | `false`            | `精准匹配：` + 武器中文名，多把用 `、` 连接；若无武器列表则为 `精准匹配` |
| `MatchFuturePromising`      | 通常为空      | 三槽为 OCR 技能文本；`SkillIDs` 为 `0,0,0`        | `LockFuturePromising` | `false`            | `未来可期：总等级 … ≥ …`                           |
| `MatchSlot3Level3Practical` | 视规则而定     | 规范槽位技能                                   | `LockSlot3Practical`  | `false`            | `实用基质：词条3(…)等级 … ≥ …`                      |
| `MatchNone`                 | 空         | `SkillIDs` 空；`SkillsChinese` 仍为 OCR 三槽文本 | `false`               | `DiscardUnmatched` | 固定 `未匹配`                                   |


未命中时废弃与否只看 `ShouldDiscard`（由 `DiscardUnmatched` 决定），与 `Reason` 文案无关。