"""离线原表 -> 发布 -> HTTP 下载 -> 任务生成的回归测试；不访问外部服务。"""

import json
import subprocess
import tempfile
import threading
import unittest
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

from click.testing import CliRunner

from . import fetch
from .cli import main
from .datasets.base import DatasetError
from .datasets.stock_staple import StockStaple
from .project import dump_json, payload_hash, payload_of
from .utils.game_data import _text_table, read_table_cfg


REPO = Path(__file__).resolve().parents[2]
FILE = "stock_staple.json"


def source_tables():
    """两档同物品、另一个物品、一次性商品、弹性商品，共五条 SKU。"""
    goods = {}
    for index, (item, price, refresh, shop) in enumerate([
        ("item_food_test", 100, 2, "stable"),
        ("item_food_test", 80, 2, "stable"),
        ("item_gem_stable_energy_point_test", 200, 2, "stable"),
        ("item_once", 10, 0, "stable"),
        ("item_random", 10, 2, "random"),
    ]):
        goods[f"sku_{index}"] = {
            "shopId": shop, "rewardId": item, "price": price, "cnDiscount": 1,
            "limitCountRefreshType": refresh, "moneyId": "item_money", "goodsTagId": "household",
            "limitCount": 0, "sortId": index, "unlockConditions": [],
        }
    item_ids = list(dict.fromkeys(row["rewardId"] for row in goods.values()))
    return {
        "DomainDataTable": {"domain_1": {"domainShopGroupId": "group", "sortId": 1, "domainName": {"id": 1}}},
        "ShopGroupTable": {"group": {"shopIds": ["stable", "random"]}},
        "ShopTable": {
            "stable": {"shopRefreshType": 0, "shopGoodsIds": [f"sku_{i}" for i in range(4)]},
            "random": {"shopRefreshType": 2, "shopGoodsIds": ["sku_4"]},
        },
        "ShopGoodsTable": goods,
        "RewardTable": {item: {"probItemBundles": [], "itemBundles": [{"id": item, "count": 1}]} for item in item_ids},
        "ItemTable": {
            item: {"name": {"id": i + 3}, "iconId": item}
            for i, item in enumerate([*item_ids, "item_money"])
        },
        "ShopGoodsTagTable": {"household": {"tagName": {"id": 2}, "sortId": 1}},
        "ShopChannelDevelopmentTable": {"channel": {"channelLevelMap": {"1": {
            "newGoodsList": ["sku_0", "sku_1", "sku_2"], "newGoodsNum": [5, 10, 20],
            "versionStart": "test", "timeId": "",
        }}}},
        **{f"I18nTextTable_{lang}": {str(i): f"{lang} name {i}" for i in range(1, 9)} for lang in ("CN", "TC", "EN", "JP", "KR")},
    }


class QuietHandler(SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass


class StockStapleTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tables = self.root / "TableCfg"
        self.published = self.root / "published"
        self.raw = self.root / "download"
        self.consumer = self.root / "consumer"
        self.data = self.consumer / "tools/zmdmap/data"
        self.source = source_tables()
        self.write_tables()

    def write_tables(self):
        self.tables.mkdir(exist_ok=True)
        for name, data in self.source.items():
            (self.tables / f"{name}.json").write_text(dump_json(data), encoding="utf-8")
        read_table_cfg.cache_clear()
        _text_table.cache_clear()

    def cli(self, *args, ok=True):
        result = CliRunner().invoke(main, list(map(str, args)))
        self.assertEqual(result.exit_code == 0, ok, result.output + repr(result.exception))
        return result

    def publish(self):
        self.write_tables()
        self.cli("generate", "--only", "stock_staple", "--table-cfg-dir", self.tables,
                 "--workspace", self.root, "--out-dir", self.published, "--raw-only", "--version", "1.0.test")
        return json.loads((self.published / FILE).read_text(encoding="utf-8"))

    def serve(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), partial(QuietHandler, directory=str(self.published)))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()

        def stop():
            server.shutdown()
            thread.join()
            server.server_close()

        self.addCleanup(stop)
        for override in (
            patch.object(fetch, "DATA_BASE_URL", f"http://127.0.0.1:{server.server_port}"),
            patch.object(fetch, "fetch_latest_version", side_effect=AssertionError("external API called")),
        ):
            override.start()
            self.addCleanup(override.stop)

    def fetch(self, ok=True):
        return self.cli("fetch", "--only", "stock_staple", "--out-dir", self.raw, "--data-dir", self.data, ok=ok)

    def generate(self, ok=True):
        # 与 pnpm 脚本同一个入口，只把输出根放进临时目录，避免修改工作树。
        code = "const {generate}=await import(process.argv[1]); await generate({root:process.argv[2]});"
        result = subprocess.run(["node", "--input-type=module", "-e", code,
                                 (REPO / "tools/pipeline-generate/AutoStockStaple/generate.mjs").as_uri(),
                                 str(self.consumer)], capture_output=True, text=True, encoding="utf-8")
        self.assertEqual(result.returncode == 0, ok, result.stdout + result.stderr)

    def artifacts(self):
        return {p.relative_to(self.consumer).as_posix(): p.read_bytes() for p in (self.consumer / "assets").rglob("*.json")}

    def test_selects_recurring_goods_and_keeps_price_tiers(self):
        data = self.publish()
        self.assertEqual(set(data["items"]), {"item_food_test", "item_gem_stable_energy_point_test"})
        self.assertEqual([row["price"] for row in data["goods"].values()], [100, 80, 200])
        self.assertEqual(data["goods"]["sku_1"]["channel_supply"][0]["count"], 10)
        self.assertEqual(data["sha256"], payload_hash(payload_of(data)))
        self.assertEqual(len(data["items"]["item_food_test"]["names"]), 5)
        # TableCfg 枚举名和数值两种序列化形态得到同样的商品集合。
        self.source["ShopTable"]["stable"]["shopRefreshType"] = "None"
        self.source["ShopGoodsTable"]["sku_3"]["limitCountRefreshType"] = "None"
        self.assertEqual(self.publish(), data)
        (self.published / FILE).write_bytes(b"\xff\xfe")
        self.assertEqual(self.publish(), data)

    def test_checked_in_outputs_and_runtime_links(self):
        self.data.mkdir(parents=True)
        source = (REPO / "tools/zmdmap/data" / FILE).read_bytes()
        (self.data / FILE).write_bytes(source)
        self.generate()
        data = json.loads(source)
        outputs = self.artifacts()
        for relative, generated in outputs.items():
            if "locales" not in relative:
                self.assertEqual(generated, (REPO / relative).read_bytes(), f"stale generated file: {relative}")
        task = json.loads(outputs["assets/tasks/AutoStockStaple.json"])
        nodes = {}
        for name in ("Goods", "GoodsCountValidate", "QuantityControl"):
            nodes.update(json.loads(outputs[f"assets/resource/pipeline/AutoStockStaple/General/{name}.json"]))
        cases = [case for name, option in task["option"].items() if name.endswith("Items") for case in option["cases"]]
        selected = {item for case in cases for item in case["pipeline_override"]["AutoStockInStapleItemName"]["attach"]}
        self.assertEqual(selected, set(data["items"]))
        self.assertEqual(len({case["name"] for case in cases}), len(cases))
        self.assertEqual(len(cases), len(data["items"]))
        limits = {node: value for name, option in task["option"].items() if name.endswith("Limit")
                  for node, value in option["pipeline_override"].items()}
        branches = nodes["AutoStockStapleQuantityControl"]["next"]
        self.assertEqual(len(branches), len(cases))
        for branch in branches:
            self.assertEqual(nodes[branch]["next"], [branch + "StockBillInsufficient", branch + "Buy", branch + "Exclude"])
            buy = nodes[branch + "Buy"]
            args = buy["action"]["param"]["custom_action_param"]
            item = args["item_name"]
            validator = args["validator_node"]
            self.assertIn(item, data["items"])
            self.assertIn(validator, limits)
            self.assertIn(validator, buy["recognition"]["param"]["all_of"])
            self.assertEqual(nodes[branch + "RemoveFilter"]["action"]["param"]["custom_action_param"]["patch"]
                             ["AutoStockInStapleItemName"]["attach"], {item: False})
        # 每个本任务的界面和提示文本都能从独立生成的语言产物解析。
        def strings(value):
            if isinstance(value, str):
                yield value
            elif isinstance(value, list):
                for child in value:
                    yield from strings(child)
            elif isinstance(value, dict):
                for child in value.values():
                    yield from strings(child)
        refs = {value[1:] for value in strings([task, nodes]) if value.startswith("$")}
        for locale in ("zh_cn", "zh_tw", "en_us", "ja_jp", "ko_kr"):
            messages = json.loads(outputs[f"assets/locales/interface/{locale}.json"])
            self.assertFalse(refs - messages.keys(), f"unresolved {locale} texts")

    def test_missing_or_invalid_source_does_not_publish_partial_data(self):
        original = self.publish()
        for label, mutate in [
            ("missing language", lambda t: t["I18nTextTable_KR"].pop("3")),
            ("dangling reward", lambda t: t["RewardTable"].pop("item_food_test")),
            ("negative price", lambda t: t["ShopGoodsTable"]["sku_0"].update(price=-1)),
            ("non-finite price", lambda t: t["ShopGoodsTable"]["sku_0"].update(price=float("nan"))),
            ("mismatched supply", lambda t: t["ShopChannelDevelopmentTable"]["channel"]["channelLevelMap"]["1"].update(newGoodsNum=[])),
        ]:
            with self.subTest(label=label):
                self.source = source_tables()
                mutate(self.source)
                self.write_tables()
                with self.assertRaises(DatasetError):
                    StockStaple.build(self.tables, self.root / "absent")
                self.cli("generate", "--only", "stock_staple", "--table-cfg-dir", self.tables,
                         "--workspace", self.root, "--out-dir", self.published, "--raw-only", ok=False)
                self.assertEqual(json.loads((self.published / FILE).read_text(encoding="utf-8")), original)

    def test_full_chain_rebuilds_empty_corrupt_and_missing_outputs(self):
        self.publish()
        self.serve()
        self.fetch()
        self.generate()
        expected = self.artifacts()
        self.assertEqual(len(expected), 9)  # task + 3 pipeline + 5 locales
        # 本地缓存和入库副本损坏，全部下游缺失；一次 fetch + generate 完整恢复。
        for corrupt in (b"{broken", b"\xff\xfe", b'{"version":"1.0.test","items":{}}'):
            with self.subTest(corrupt=corrupt):
                (self.raw / FILE).write_bytes(corrupt)
                (self.data / FILE).write_bytes(corrupt)
                for relative in expected:
                    (self.consumer / relative).unlink()
                self.fetch()
                self.generate()
                self.assertEqual(self.artifacts(), expected)
        # 下载缓存还在，但入库数据不存在；不能因为跳过下载就漏掉复制。
        (self.data / FILE).unlink()
        self.fetch()
        # 任务专属输出即使变成无效 JSON，也从模板覆盖重建。
        for relative in expected:
            if "locales" not in relative:
                (self.consumer / relative).write_text("broken", encoding="utf-8")
        self.generate()
        self.assertEqual(self.artifacts(), expected)
        # 共享语言文件保留其他任务，但本任务残旧或错误的键应重建。
        locale = self.consumer / "assets/locales/interface/zh_cn.json"
        messages = json.loads(locale.read_text(encoding="utf-8"))
        messages.update({"unrelated.task": "keep", "task.AutoStockStaple.item.item_food_test": "wrong",
                         "task.AutoStockStaple.item.removed": "stale"})
        locale.write_text(dump_json(messages), encoding="utf-8")
        self.generate()
        rebuilt = json.loads(locale.read_text(encoding="utf-8"))
        self.assertEqual(rebuilt.pop("unrelated.task"), "keep")
        self.assertEqual(rebuilt, json.loads(expected["assets/locales/interface/zh_cn.json"]))

    def test_same_version_update_adds_removes_and_renames_without_binding_table(self):
        self.publish()
        self.serve()
        self.fetch()
        self.generate()
        before = self.artifacts()
        # 同版本新增物品并替换旧物品；两条 SKU 同步变更，不手工增加任何消费端绑定。
        for sku in ("sku_0", "sku_1"):
            self.source["ShopGoodsTable"][sku]["rewardId"] = "item_food_new"
            self.source["ShopGoodsTable"][sku]["price"] += 7
        self.source["RewardTable"]["item_food_new"] = {"probItemBundles": [], "itemBundles": [{"id": "item_food_new", "count": 1}]}
        self.source["ItemTable"]["item_food_new"] = {"name": {"id": 3}, "iconId": "new"}
        for lang in ("CN", "TC", "EN", "JP", "KR"):
            self.source[f"I18nTextTable_{lang}"]["3"] = f"{lang} renamed"
        published = self.publish()
        self.fetch()
        self.generate()
        after = self.artifacts()
        self.assertNotEqual(before, after)
        self.assertEqual(json.loads((self.data / FILE).read_text(encoding="utf-8")), published)
        combined = b"".join(after.values()).decode("utf-8")
        self.assertIn("item_food_new", combined)
        self.assertIn("KR renamed", combined)
        self.assertNotIn("item_food_test", combined)
        self.generate()
        self.assertEqual(self.artifacts(), after)

    def test_bad_download_fails_without_replacing_good_cache(self):
        self.publish()
        self.serve()
        self.fetch()
        original = (self.raw / FILE).read_bytes()
        self.source["ShopGoodsTable"]["sku_0"]["price"] += 1
        published = self.publish()
        for invalid in ("not json", dump_json({**published, "items": {}}), dump_json({**published, "sha256": "wrong"})):
            with self.subTest(invalid=invalid[:30]):
                (self.published / FILE).write_text(invalid, encoding="utf-8")
                self.fetch(ok=False)
                self.assertEqual((self.raw / FILE).read_bytes(), original)
                self.assertEqual((self.data / FILE).read_bytes(), original)
        # 源数据失效时，消费端必须先报错，不写一半下游。
        self.generate()
        outputs = self.artifacts()
        (self.data / FILE).write_text('{"items": {}}', encoding="utf-8")
        self.generate(ok=False)
        self.assertEqual(self.artifacts(), outputs)


if __name__ == "__main__":
    unittest.main()
