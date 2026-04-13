# i18n 同步与翻译说明

`tools/i18n/sync_locale_text.py` 现在拆分为两个阶段，默认只做生成，不自动翻译。
推荐流程是：先生成空翻译 -> 人工先完成任意一种语言 -> 再让 LLM 自动补全其他语言。

## 1. 本地 LLM 配置

在 `tools/i18n/` 下新建 `llm.json`（该文件已加入 `.gitignore`，不会被提交）：

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

## 2. 仅生成缺失 key（默认行为）

```powershell
python tools/i18n/sync_locale_text.py --phase generate --write
```

用途：

- 扫描 `assets/tasks` 与 `assets/interface.json` 中的 `$KEY`
- 在 `assets/locales/interface/*.json` 中补齐缺失 key（值为 `""`）

## 3. 人工先翻译一种语言

在第 2 步执行后，任选一种语言（例如 `zh_cn`）手动把新增 key 的文案补齐。

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
- `--phase translate`：与输入 `TR` 等价，都是进入翻译阶段
- `--fail-on-translate-error`：翻译有错误时返回非零退出码
