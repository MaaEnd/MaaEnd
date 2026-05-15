"""
物品中文名反查工具 - 根据中文名称查找物品的 iconId 及多语言翻译

用法：
  # 带参数（输出 JSON）：
  python lookup.py --data <EndfieldData路径> --i18n <i18n仓库路径> "低容武陵电池" "蓝铁粉末"

  # 不带参数（交互式）：
  python lookup.py

输出字段：
  query   - 查询的中文名
  itemId  - 物品在 ItemTable 中的 id
  iconId  - 物品图标文件名（对应 item_xxx.png）
  CN/TC/EN/JP/KR - 五种语言的物品名称
  rarity  - 稀有度（1-5）
"""
import argparse
import json
import sys
from pathlib import Path

LANGS = ["CN", "TC", "EN", "JP", "KR"]


def load_json(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def build_cn_reverse(cn_data):
    cn_text_to_ids = {}
    for tid, text in cn_data.items():
        if text:
            cn_text_to_ids.setdefault(text, []).append(tid)
    return cn_text_to_ids


def build_name_id_index(item_table):
    name_id_to_items = {}
    for item_id, item in item_table.items():
        tid = str(item.get("name", {}).get("id", ""))
        icon_id = item.get("iconId", "")
        if tid and tid != "0" and not item_id.startswith(("sysbp_", "item_factech_", "item_fbottle_", "item_formula_")):
            name_id_to_items.setdefault(tid, []).append((item_id, icon_id, item.get("rarity", 0)))
    return name_id_to_items


def lookup(names, item_table_path, i18n_dir):
    item_table = load_json(item_table_path)
    name_id_index = build_name_id_index(item_table)

    i18n_data = {}
    for lang in LANGS:
        i18n_data[lang] = load_json(i18n_dir / f"I18nTextTable_{lang}.json")

    cn_reverse = build_cn_reverse(i18n_data["CN"])
    results = []

    for name in names:
        tids = cn_reverse.get(name)
        if not tids:
            results.append({"query": name, "error": "未找到匹配项"})
            continue
        for tid in tids:
            items = name_id_index.get(tid, [])
            if not items:
                continue
            translations = {lang: i18n_data[lang].get(tid, "") for lang in LANGS}
            for item_id, icon_id, rarity in items:
                results.append({
                    "query": name,
                    "itemId": item_id,
                    "iconId": icon_id,
                    "rarity": rarity,
                    **translations,
                })

    return results


def interactive(item_table_path, i18n_dir):
    item_table = load_json(item_table_path)
    name_id_index = build_name_id_index(item_table)

    i18n_data = {}
    for lang in LANGS:
        p = i18n_dir / f"I18nTextTable_{lang}.json"
        print(f"加载 i18n: {p}")
        i18n_data[lang] = load_json(p)

    cn_reverse = build_cn_reverse(i18n_data["CN"])
    print(f"就绪。CN 文本条目: {len(cn_reverse)}，物品条目: {len(item_table)}")
    print("输入物品中文名（多个用空格分隔），空行退出：\n")

    while True:
        try:
            line = input("> ").strip()
        except (EOFError, KeyboardInterrupt):
            print()
            break

        if not line:
            break

        names = line.split()
        for name in names:
            tids = cn_reverse.get(name)
            if not tids:
                print(f"  未找到匹配项: {name}")
                continue
            for tid in tids:
                items = name_id_index.get(tid, [])
                translations = [i18n_data[lang].get(tid, "") for lang in LANGS]
                if not items:
                    continue
                for item_id, icon_id, rarity in items:
                    parts = [icon_id, item_id, str(rarity)] + translations
                    print("  " + " | ".join(parts))


def main():
    parser = argparse.ArgumentParser(description="物品中文名反查 iconId 及多语言名称")
    parser.add_argument("--data", help="EndfieldData 仓库路径（含 TableCfg/）")
    parser.add_argument("--i18n", help="i18n 仓库路径（含 TableCfg/）")
    parser.add_argument("names", nargs="*", help="要查询的物品中文名")
    args = parser.parse_args()

    if args.data and args.i18n and args.names:
        item_table_path = Path(args.data) / "TableCfg" / "ItemTable.json"
        i18n_dir = Path(args.i18n) / "TableCfg"
        results = lookup(args.names, item_table_path, i18n_dir)
        print(json.dumps(results, ensure_ascii=False, indent=2))
    else:
        repo1 = input("EndfieldData 仓库路径: ").strip().strip('"')
        repo2 = input("i18n 仓库路径: ").strip().strip('"')
        item_table_path = Path(repo1) / "TableCfg" / "ItemTable.json"
        i18n_dir = Path(repo2) / "TableCfg"
        print(f"加载 ItemTable: {item_table_path}")
        interactive(item_table_path, i18n_dir)


if __name__ == "__main__":
    main()
