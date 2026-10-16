"""zmdmap 里跨模块共用的常量。

语言是这里最容易写歪的一处：同一批语言，在几处地方用不同写法——

| 用处 | 语言 key | 例子 |
| --- | --- | --- |
| 文本表文件 ``I18nTextTable_<后缀>.json`` | 后缀 | ``CN`` |
| EssenceFilter / IconRecognition 读的数据（``weapons`` / ``item_mini_table``） | 后缀 | ``CN`` |
| 入 pipeline 的数据（MaaEnd 生成器读的） | locale 名 | ``zh_cn`` |

所以这里只写映射（:data:`I18N_TABLE_SUFFIXES`、:data:`LOCALE_NAMES`），其余由它们推出来。
"""

from __future__ import annotations

#: 语言代码 -> ``I18nTextTable_<后缀>.json`` 的后缀；也就是 ``weapons`` / ``item_mini_table`` 里的语言 key。顺序固定。
I18N_TABLE_SUFFIXES: dict[str, str] = {
    "zh-CN": "CN",
    "zh-TW": "TC",
    "en-US": "EN",
    "ja-JP": "JP",
    "ko-KR": "KR",
}

#: 语言代码 -> MaaEnd 的 locale 名（``assets/locales/`` 里的文件名，也是数据文件的语言 key）。
LOCALE_NAMES: dict[str, str] = {
    "zh-CN": "zh_cn",
    "zh-TW": "zh_tw",
    "en-US": "en_us",
    "ja-JP": "ja_jp",
    "ko-KR": "ko_kr",
}

#: 入 pipeline 数据的 ``(语言 key, 语言代码)``。
MAAEND_LOCALES: tuple[tuple[str, str], ...] = tuple(
    (name, lang) for lang, name in LOCALE_NAMES.items()
)

#: 用文本表后缀那一套语言 key 的数据的 ``(语言 key, 语言代码)``。
UPSTREAM_LOCALES: tuple[tuple[str, str], ...] = tuple(
    (suffix, lang) for lang, suffix in I18N_TABLE_SUFFIXES.items()
)

# 数据来源：zmdmap 发布的精简数据。
# 数据与图标都从 voice.fz.wiki 取：它和 assets.fz.wiki 是同一个源站，但前置的 EdgeOne 有海外
# 节点，GitHub runner 不必再绕回国内 CDN（那边每个文件都要重新握手，几秒起步、偶发超时）。
VERSION_API = "https://api.zmdmap.com/api/v1/endfield/version"
DATA_BASE_URL = "https://voice.fz.wiki/output_maaend"
USER_AGENT = "MaaEnd-pipeline"

#: 发布目录里的清单（``<DATA_BASE_URL>/manifest.json``）：这批数据是哪一版、每份原始形态的内容指纹。
#: 有它就不必再问版本接口，也能认出一份数据在同一个版本号下被重新发布过。
MANIFEST_FILE = "manifest.json"
