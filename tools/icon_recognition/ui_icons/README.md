# IconRecognition UI 图标

该目录保存从已发布 IconRecognition catalog 生成 UI 图标所需的配置、脚本和合成素材。

独立运行：

```powershell
uv run --group images python -m tools.icon_recognition.ui_icons.generate
```

默认读取 `assets/data/IconRecognition/recognition_items.json` 和
`assets/resource/image/IconRecognition/`，将 32x32 图标输出到
`assets/resource/image/IconRecognition/UI/`。输出文件名使用 catalog 顶层
`item_id`。已有同名文件会先检查尺寸，尺寸为 32x32 或尺寸异常时都跳过，避免覆盖
发布后的图片优化结果。

`config.jsonc` 使用 IconRecognition 的筛选语义：

- `item_filters`：基础分类筛选；
- `item_ids`：可选的 ID 交集；
- `additional_item_filters`：额外并入的分类；
- `excluded_item_ids`：最后排除的 ID。
- `exclude_rules`：按物品分类和嵌套子规则排除物品。例如贵重品库武器只保留 5、6 星：

```jsonc
{
    "item_filter": "ValuableDepot:Weapon",
    "sub_rules": [
        {
            "rarity": {
                "not_in": [
                    5,
                    6
                ]
            }
        }
    ]
}
```

`rarity` 子规则也支持 `in`，表示稀有度命中数组时排除。每个 `rarity` 子规则必须且只能配置 `in` 或 `not_in`。
命中排除规则的物品会被忽略；脚本不会自动删除之前已经生成的同名图片。

完整 publish 流程会在 catalog 和普通识别图标发布完成后自动调用该生成器。
