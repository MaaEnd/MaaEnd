# IconRecognition 资源下载与发布

本目录维护物品数据、五语言名称和图标下载工具。最终运行资源发布到 `assets`，生产代码与测试均不读取工具缓存。

## 数据来源

远端数据来自终末地社区 [wiki](https://fz.wiki)，当前使用以下资源：

- `https://assets.fz.wiki/output_beyondmap/item_mini_table.json`
- `https://assets.fz.wiki/output_maaend/weapons.json`
- `https://assets.fz.wiki/output_beyondmap/i18n/{locale}/lang.json`
- `https://assets.fz.wiki/output_image/itemicon/<url-encoded-iconId>.png@raw`

普通物品语言路径使用 `zh-CN/zh-TW/en-US/ja-JP/ko-KR`，武器名称读取 `weapons.json` 的 `CN/TC/EN/JP/KR` key。

## 下载

```powershell
python tools/icon_recognition/download.py
```

只列出将访问的远端 JSON，不写缓存：

```powershell
python tools/icon_recognition/download.py --dry-run
```

下载结果写入 `tools/icon_recognition/.cache/downloads/`。该目录只作为下载工具缓存，不参与生产或测试运行。远端表和语言文件每次运行都会校验；有效 PNG 按 `iconId` 增量复用。

物品黑名单维护在 `tools/icon_recognition/blacklist.json`，按 `storageKind`、`categoryType` 和物品 ID 规则过滤，不直接写入下载脚本。

上游表中缺失但必须发布的固定物品集中维护在 `tools/icon_recognition/fixed_items.json`。下载、catalog 和多语言生成都从该文件读取，不要在各脚本内重复硬编码物品字段。

原始图标必须是正方形，边长必须为 2 的整数次幂，例如 128x128 或 256x256。遇到非标准图片时下载失败并写入报告，不执行自动拉伸或补边。

## 生成发布资源

缓存完整后，一条命令同时生成 catalog 和五语言名称：

```powershell
python tools/icon_recognition/publish.py
```

只同步 `fixed_items.json` 中的图标、catalog 和多语言名称，不引入本次上游全量数据变化：

```powershell
python tools/icon_recognition/publish.py --fixed-only
```

默认输入：

- `tools/icon_recognition/.cache/downloads/item.json`
- `tools/icon_recognition/.cache/downloads/item_mini_table.json`
- `tools/icon_recognition/.cache/downloads/weapons.json`
- `tools/icon_recognition/.cache/downloads/lang_*.json`
- `tools/icon_recognition/.cache/downloads/images/`

默认输出：

- `assets/data/IconRecognition/recognition_items.json`
- `assets/locales/interface/{zh_cn,zh_tw,en_us,ja_jp,ko_kr}.json`

有特殊目录需求时运行 `python tools/icon_recognition/publish.py --help` 覆盖单个路径。`catalog.py` 与 `localization.py` 仍可用于单独调试生成阶段，普通发布不需要分别调用。

## 发布数据结构

`recognition_items.json` 的顶层 key 是识别 API 使用的 `item_id`。每项包含：

| 字段 | 说明 |
| --- | --- |
| `name` | 对应 i18n 的中文名称，仅用于提高 catalog 可读性；运行时名称仍按 `item_id` 查询 locale |
| `category` | 中文分类标签 |
| `storageKind` / `categoryType` | 候选过滤分类 |
| `rarity` | 图标目录与物品稀有度 |
| `sortId1` / `sortId2` | 上游物品排序字段；仅 mini table 物品包含，武器和固定物品不补默认值 |
| `iconId` | 原始图标文件名，不等同于 `item_id` |
| `fluidIconId` | 复合图标的内容物图标；普通图标为空 |
| `regionRestricted` | 可选 boolean；仅上游明确为 true 时导出，表示物品可能因地区限制显示为禁用态 |

运行时多语言 key 为 `iconRecognition.name.<item_id>`。发布脚本会删除 locale 中已不在 catalog 的旧 key，并要求五语言 key 数与 catalog 完全一致。

固定物品的 `iconId`、i18n key、rarity、`storageKind` 和 `categoryType` 均以 `fixed_items.json` 为唯一事实来源。当前 9 项都使用“独立资源”分类，并发布到各自的 `Isolate:<categoryType>` 候选集。

最终图标位于 `assets/resource/image/IconRecognition/<rarity>/<iconId>.png`。识别时从这里的 128 或 256 原图直接缩放到目标 cell 尺寸，不经过固定中间尺寸。

## 校验与故障恢复

```powershell
python -m unittest discover -s tools/icon_recognition -p "test_*.py"
```

下载失败时保留已成功缓存的文件，并把失败项写入 `download_report.json`。按项目约定等待 10 分钟后重试原下载命令；发布阶段可离线复用已经完整校验的缓存。
