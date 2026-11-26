import unittest
from unittest.mock import patch

from tools.AutoCollectRoute import registry


class TeleportSceneTest(unittest.TestCase):
    def setUp(self) -> None:
        self.route = registry.Route(id="TestRoute1", region="TestRegion", items=(), number=1)
        self.teleport = "SceneEnterWorldTestAnchor"
        route_document = {
            "AutoCollectTestRoute1Start": {
                "action": "Custom",
                "custom_action": "SubTask",
                "custom_action_param": {"sub": [self.teleport]},
            },
        }
        self.enterContext(patch.object(registry, "read_jsonc", return_value=route_document))

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


class StockPanelPollingTest(unittest.TestCase):
    def test_clicks_poll_only_for_the_expected_panel_states(self) -> None:
        route = registry.Route(id="TestRoute1", region="TestRegion", items=(), number=1)
        area = registry.StockArea((0, 0), "RareCollectArea", "ListEntry.png", "SceneEnterMapTestRegion")
        nodes = registry.stock_nodes(route, "Test Route", route.region, area)
        panel = f"__AutoCollect{route.id}StockPanel"
        recognition_types = {"Empty": "OCR", "Stocked": "OCR", "Pick": "TemplateMatch"}

        for step, candidates in (
            ("Open", ["Empty", "Stocked", "Pick"]),
            ("Pick", ["Empty", "Stocked"]),
        ):
            with self.subTest(step=step):
                click = nodes[f"{panel}{step}"]
                self.assertEqual(click["next"], [f"{panel}{state}" for state in candidates])
                self.assertEqual(click["on_error"], [registry.STOCK_ABORTED])
                self.assertEqual(click["timeout"], 5_000)
                self.assertGreater(click["timeout"], click["rate_limit"])
                for state in candidates:
                    self.assertEqual(nodes[f"{panel}{state}"]["recognition"]["type"], recognition_types[state])
