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
