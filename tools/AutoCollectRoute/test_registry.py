import unittest
from unittest.mock import patch

from tools.AutoCollectRoute import registry


class TeleportSceneTest(unittest.TestCase):
    def setUp(self) -> None:
        self.route = registry.Route(id="TestRoute1", region="TestRegion", items=(), number=1)
        self.teleport = "SceneEnterWorldTestAnchor"
        self.route_document = {
            "AutoCollectTestRoute1Start": {
                "action": "Custom",
                "custom_action": "SubTask",
                "custom_action_param": {"sub": [self.teleport]},
            },
        }
        self.enterContext(patch.object(registry, "read_jsonc", return_value=self.route_document))

    def test_read_supported_next_forms(self) -> None:
        expected = "SceneEnterMapTestRegion"
        jump_back = {"name": expected, "jump_back": True}
        node_values = (
            expected,
            f"[JumpBack]{expected}",
            jump_back,
            ["SceneTeleportConfirm", jump_back, "[JumpBack]SceneEnterMapAny"],
        )
        for value in node_values:
            with self.subTest(next=value):
                self.assertEqual(registry.teleport_scene(self.route, {self.teleport: {"next": value}}), expected)

    def test_missing_map_scene(self) -> None:
        self.assertIsNone(registry.teleport_scene(self.route, {}))

    def test_multiple_start_subtasks_do_not_select_stock_area(self) -> None:
        self.route_document["AutoCollectTestRoute1Start"]["custom_action_param"]["sub"].append("CollectInAnotherArea")
        scenes = {self.teleport: {"next": "[JumpBack]SceneEnterMapTestRegion"}}
        self.assertIsNone(registry.teleport_scene(self.route, scenes))


class RouteCollectPointsTest(unittest.TestCase):
    def test_schema_metadata_preserves_collect_points(self) -> None:
        route = registry.Route(id="TestRoute1", region="Wuling", items=(), number=1)
        document = {
            "$schema": "../../../../../tools/schema/pipeline.schema.json",
            "AutoCollectTestRoute1Goto": {
                "action": "Custom",
                "custom_action": "MapNavigateAction",
                "custom_action_param": {"path": [{"action": "ZONE", "zone_id": "Wuling_Base"}, [10, 20, "COLLECT"]]},
            },
        }
        with patch.object(registry, "read_jsonc", return_value=document):
            self.assertEqual(registry.route_collect_points(route), [(10, 20)])


class StockPanelPollingTest(unittest.TestCase):
    def test_clicks_poll_before_trying_to_close_the_panel(self) -> None:
        route = registry.Route(id="TestRoute1", region="TestRegion", items=(), number=1)
        panel = f"__AutoCollect{route.id}StockPanel"
        panel_states = [f"{panel}Empty", "AutoCollectStockPanelStocked"]
        exit_nodes = ["AutoCollectStockPanelClose", "AutoCollectStockPanelLeave"]
        shared = registry.stock_panel_shared_nodes()
        self.assertEqual(shared["AutoCollectStockPanelStocked"]["next"], exit_nodes)
        self.assertEqual(shared["AutoCollectStockPanelClose"]["on_error"], [exit_nodes[1]])
        self.assertEqual(shared["AutoCollectStockPanelLeave"]["recognition"], "DirectHit")

        for icon, template, list_node in (
            ("RareCollectArea", "AutoCollect/RareCollectListEntry.png", "AutoCollectStockPanelRareCollectListEntry"),
            ("RareMineralArea", "AutoCollect/RareMineralListEntry.png", "AutoCollectStockPanelRareMineralListEntry"),
        ):
            area = registry.StockArea((0, 0), icon, template, "SceneEnterMapTestRegion")
            nodes = registry.stock_nodes(route, "Test Route", route.region, area)
            with self.subTest(icon=icon):
                find = f"__AutoCollect{route.id}StockFind"
                self.assertEqual(nodes[find]["next"], [f"{panel}Open"])
                self.assertEqual(nodes[f"{panel}Open"]["action"]["param"]["target"], find)
                self.assertEqual(nodes[f"{panel}Empty"]["all_of"], ["AutoCollectStockPanelEmpty"])
                self.assertEqual(nodes[f"{panel}Pick"]["all_of"], [list_node])
                self.assertEqual(shared[list_node]["recognition"]["param"]["template"], [template])
                for step, candidates in (("Open", [*panel_states, f"{panel}Pick"]), ("Pick", panel_states)):
                    click = nodes[f"{panel}{step}"]
                    self.assertEqual(click["next"], candidates)
                    self.assertEqual(click["on_error"], exit_nodes)
                    self.assertEqual(click["timeout"], 5_000)
                    self.assertGreater(click["timeout"], click["rate_limit"])

    def test_screenshot_nodes_remain_when_no_route_checks_stock(self) -> None:
        cases = registry.read_jsonc(registry.PROJECT_ROOT / "tests" / "AutoCollect" / "test_stock_panel.json")["cases"]
        screenshot_nodes = {hit if isinstance(hit, str) else hit["node"] for case in cases for hit in case["hits"]}
        document = registry.dispatch_document([], [], {}, {})
        self.assertTrue(screenshot_nodes)
        self.assertEqual(screenshot_nodes - document.keys(), set())
