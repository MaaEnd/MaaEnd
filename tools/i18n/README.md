# i18n 同步与翻译说明

`tools/i18n/sync_locale_text.py` 现在支持三类来源：

- `assets/interface.json` 与 `assets/tasks/**/*.json` 中的旧式 `$KEY`
- `assets/resource/pipeline/**`、`assets/resource_adb/pipeline/**`、`assets/resource_wlroots/pipeline/**` 中的旧式 `$KEY`
- `assets/interface.json` 与 `assets/tasks/**/*.json` 中的内联原文标记 `$开发者母语$`

脚本仍然拆分为“生成”和“翻译”两个阶段，但默认生成阶段已经会：

- 自动补齐 locale 中缺失的 key
- 将内联 `$开发者母语$` 自动改写为生成后的 `$KEY`
- 把原文写入开发者语言对应的 locale，供后续翻译阶段使用

## 1. 本地 LLM 配置

本地可在 `tools/i18n/` 下新建 `llm.json`（该文件已加入 `.gitignore`，不会被提交）：

```json
{
    "api_base_url": "https://api.deepseek.com/v1",
    "api_key": "sk-xxx",
    "model": "deepseek-chat"
}
```

说明：

- `api_base_url`：兼容 OpenAI Chat Completions 的地址
- `api_key`：本地 API Key
- `model`：翻译使用的模型名

CI 也支持直接通过环境变量注入：

- `I18N_LLM_API_BASE_URL`
- `I18N_LLM_API_KEY`
- `I18N_LLM_MODEL`

## 2. 仅生成缺失 key（默认行为）

```powershell
python tools/i18n/sync_locale_text.py --phase generate --write
```

用途：

- 扫描 `assets/tasks`、`assets/interface.json` 与三个 pipeline 目录中的 `$KEY`
- 在 `assets/locales/interface/*.json` 中补齐缺失 key（值为 `""`）
- 将 `assets/tasks` / `assets/interface.json` 中的 `$开发者母语$` 改写为自动生成的 `$KEY`

自动生成 key 的规则：

- `interface` 文件按“类型 / 节点名 / 属性”生成，如 `auto.interface.controller.Win32-Window.label`
- `task` 文件按“task 文件名 / 类型 / 节点名 / 属性”生成，如 `auto.task.AutoStockpile.option.AutoStockpileServerTime.label`
- 若同一位置生成的 key 已存在且对应源文本不同，脚本会自动追加稳定哈希后缀避免冲突

## 3. 人工先翻译一种语言

在第 2 步执行后，任选一种语言（例如 `zh_cn`）手动把新增 key 的文案补齐。

如果你使用了 `$开发者母语$` 内联标记，并且开发者语言就是 `zh_cn`，那么这一步通常已经由脚本自动完成。

脚本在翻译阶段会逐 key 读取“已存在的非空文本”作为源文本，并把其他语言的空文本补全。

## 4. 输入 TR 开始翻译其他语言

```powershell
python tools/i18n/sync_locale_text.py TR --write
```

用途：

- 读取 locale 文件中已有的空文本条目
- 对每个 key，自动使用任意已翻译语言的非空文本作为源
- 使用 `llm.json` 的配置调用模型翻译并补全其他语言

## 5. 一次执行全流程（生成 + 翻译）

```powershell
python tools/i18n/sync_locale_text.py --phase all --write
```

## 6. 生成报告

```powershell
python tools/i18n/sync_locale_text.py --phase all --report "%TEMP%/maaend-i18n-report.json"
```

可选参数：

- `--batch-size`：每次翻译请求携带条目数
- `--llm-config`：自定义 `llm.json` 路径
- `--developer-locale`：指定内联 `$开发者母语$` 对应的语言，默认是 `zh_cn`
- `--phase translate`：与输入 `TR` 等价，都是进入翻译阶段
- `--fail-on-translate-error`：翻译有错误时返回非零退出码
