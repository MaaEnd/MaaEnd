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

- `Kind`：命中类型
- `Weapons`：exact 命中时返回匹配到的武器列表（扩展规则命中时可能为空）
- `SkillIDs` / `SkillsChinese`：技能 ID 与中文名（用于展示/统计/后续策略）
- `ShouldLock`：是否应执行“上锁”
- `ShouldDiscard`：是否应执行“废弃”
- `Reason`：人类可读原因（扩展规则命中时会给出）

