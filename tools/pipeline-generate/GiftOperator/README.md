# 干员赠礼生成器

赠礼与据点交易共用 `tools/zmdmap/data/operators.json`。下载已发布的 `sell_product.json` 后，zmdmap 入库步骤自动拆出其中完整的 `operators`，同时从入库的 `sell_product.json` 移除重复名称。公共目录是生成产物，不是手写绑定表，也不请求上游不存在的独立文件。

## 输入与产物

- `tools/zmdmap/data/operators.json`：全量游戏干员 ID 与五语言名称。赠礼仅排除 `chr_0002_endminm`、`chr_0003_endminf`，其余全部生成，顺序沿用上游目录。
- `assets/resource/image/GiftOperator/Operators/<选项名>.png` 与 `assets/resource_adb/image/GiftOperator/Operators/<选项名>.png`：运行时使用的头像模板，图片获取与转换链路待接入。
- `generate.mjs`：生成 `assets/tasks/GiftOperator.json` 的 `SelectOperator.cases`、`Operator/Operator.json` 的收礼识别节点、`GiftOperatorContact.json` 的干员分派列表，以及五语言 `operator.*` 文案。

生成器保留 `Any` 选项、其他任务选项与流程节点，以及非生成字段的 JSONC 注释。选项名、节点后缀与模板文件名共用据点交易的英文名称转 PascalCase 规则；当前已有干员的标识不变。缺少干员数据或五语言名称时停止生成。现有头像文件不参与名单筛选。

界面名称与 OCR 候选均以公共目录为唯一来源。OCR 候选按五语言顺序合并并去重，名称更新后重新生成所有对应产物。

## 使用

```shell
pnpm generate:GiftOperator

# 数据已经同步，只重新生成赠礼配置
node tools/pipeline-generate/GiftOperator/generate.mjs
```

新增干员只需同步上游数据并重新生成，不再登记 ID 或名称。若上游改变英文名称，派生的选项标识和模板路径也会变化，应在生成 diff 中审查。

`pnpm generate:GiftOperator` 先执行 `pnpm fetch:zmdmap --only sell_product`，再生成配置；`.github/workflows/sync-zmdmap.yml` 调用同一入口。更新名称后，检查送礼选项、收礼节点和五语言文案是否同步，再运行一次确认无变化。

## 草稿待办

当前数据有 34 名干员，排除男女管理员后生成 32 名，包含祀（`chr_0036_jsspsi` / `Si`）。仓库当前缺少通用与 ADB 的 `Si.png`；生成配置不等于这些干员已经可以运行。合并前需接入上游头像数据、补齐两套模板，并用实际联系人界面验证识别。不得通过过滤缺图干员缩小生成名单。

已用 Wiki 干员页的 456×564 源图试验固定偏移、缩放与两个矩形遮罩。部分干员接近现有模板，部分干员的构图有明显差异；还需确认上游能否提供游戏联系人界面使用的头像规格。图片实验尚未接入生成器，不能据此认定新干员识别已通过验证。
