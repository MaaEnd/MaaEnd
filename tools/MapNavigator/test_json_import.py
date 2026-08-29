from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from json_import import (
    discover_assert_locations,
    discover_path_routes,
    export_path_nodes,
    load_points_from_json_file,
    load_project_import_node,
    scan_project_import_nodes,
)
from model import ActionType, PathPoint, normalize_path_points


def make_point(action: ActionType, *, strict: bool = False, required: bool = False) -> PathPoint:
    point: PathPoint = {
        "x": 1242.04,
        "y": 773.41,
        "action": int(action),
        "actions": [int(action)],
        "zone": "",
        "strict": strict,
    }
    if required:
        point["required"] = True
    return point


class ExportPathNodesTest(unittest.TestCase):
    def test_exports_navmesh_as_target_object(self) -> None:
        nodes = export_path_nodes([make_point(ActionType.NAVMESH, strict=True)])

        self.assertEqual(
            nodes,
            [
                {
                    "action": "NAVMESH",
                    "target": [
                        1242.04,
                        773.41,
                    ],
                }
            ],
        )

    def test_keeps_regular_action_array_format(self) -> None:
        nodes = export_path_nodes([make_point(ActionType.INTERACT, strict=True)])

        self.assertEqual(nodes, [[1242.04, 773.41, "INTERACT", True]])

    def test_exports_required_navmesh_boundary(self) -> None:
        nodes = export_path_nodes([make_point(ActionType.NAVMESH, required=True)])

        self.assertEqual(
            nodes,
            [
                {
                    "action": "NAVMESH",
                    "target": [
                        1242.04,
                        773.41,
                    ],
                    "required": True,
                }
            ],
        )

    def test_preserves_navmesh_target_deck_y_on_import_and_export(self) -> None:
        routes = discover_path_routes(
            {
                "path": [
                    {
                        "action": "NAVMESH",
                        "target": [
                            1242.04,
                            773.41,
                        ],
                        "targetDeckY": "123.5",
                    }
                ]
            }
        )

        self.assertEqual(routes[0][0]["target_deck_y"], 123.5)
        self.assertEqual(
            export_path_nodes(routes[0]),
            [
                {
                    "action": "NAVMESH",
                    "target": [
                        1242.04,
                        773.41,
                    ],
                    "target_deck_y": 123.5,
                }
            ],
        )

    def test_does_not_merge_same_coordinate_with_different_target_decks(self) -> None:
        first = make_point(ActionType.NAVMESH)
        first["target_deck_y"] = 100.0
        second = make_point(ActionType.NAVMESH)
        second["target_deck_y"] = 200.0

        self.assertEqual(len(normalize_path_points([first, second])), 2)

    def test_exports_required_recorded_action_as_object(self) -> None:
        nodes = export_path_nodes([make_point(ActionType.TRANSFER, required=True)])

        self.assertEqual(
            nodes,
            [
                {
                    "action": "TRANSFER",
                    "target": [
                        1242.04,
                        773.41,
                    ],
                    "required": True,
                }
            ],
        )

    def test_import_preserves_required_route_point(self) -> None:
        routes = discover_path_routes(
            {
                "path": [
                    {
                        "action": "TRANSFER",
                        "target": [
                            1242.04,
                            773.41,
                        ],
                        "required": True,
                    }
                ]
            }
        )

        self.assertEqual(len(routes), 1)
        self.assertTrue(routes[0][0]["required"])


class GenericImportShapesTest(unittest.TestCase):
    def test_preserves_route_level_zip_option(self) -> None:
        with tempfile.NamedTemporaryFile("w", suffix=".json", encoding="utf-8", delete=False) as handle:
            json.dump({"path": [[1, 2], [3, 4]], "zip": True}, handle)
            path = Path(handle.name)
        try:
            route = load_points_from_json_file(path)
        finally:
            path.unlink(missing_ok=True)

        self.assertTrue(route.zip_enabled)

    def test_discovers_a_bare_path_array(self) -> None:
        routes = discover_path_routes([[1, 2], [3, 4, "SPRINT"]])

        self.assertEqual(len(routes), 1)
        self.assertEqual([(point["x"], point["y"]) for point in routes[0]], [(1.0, 2.0), (3.0, 4.0)])

    def test_discovers_a_single_map_navigate_action_node(self) -> None:
        routes = discover_path_routes(
            {
                "recognition": "DirectHit",
                "action": "Custom",
                "custom_action": "MapNavigateAction",
                "custom_action_param": {"path": [[5, 6]]},
            }
        )

        self.assertEqual(len(routes), 1)
        self.assertEqual((routes[0][0]["x"], routes[0][0]["y"]), (5.0, 6.0))

    def test_discovers_a_path_inside_a_complete_pipeline(self) -> None:
        routes = discover_path_routes(
            {
                "GotoTarget": {
                    "recognition": "DirectHit",
                    "action": "Custom",
                    "custom_action": "MapNavigateAction",
                    "custom_action_param": {"path": [[7, 8]]},
                }
            }
        )

        self.assertEqual(len(routes), 1)
        self.assertEqual((routes[0][0]["x"], routes[0][0]["y"]), (7.0, 8.0))

    def test_discovers_a_single_assert_location_node(self) -> None:
        locations = discover_assert_locations(
            {
                "recognition": "Custom",
                "custom_recognition": "MapLocateAssertLocation",
                "custom_recognition_param": {
                    "zone_id": "Wuling_Base",
                    "target": [1, 2, 30, 40],
                },
            }
        )

        self.assertEqual(len(locations), 1)
        self.assertEqual(locations[0].zone_id, "Wuling_Base")
        self.assertEqual(locations[0].target, (1.0, 2.0, 30.0, 40.0))


class ProjectImportNodesTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.project_root = Path(self.temp_dir.name)
        self.assets_dir = self.project_root / "assets"
        (self.assets_dir / "resource" / "pipeline").mkdir(parents=True)

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def _write(self, relative_path: str, text: str) -> Path:
        path = self.assets_dir / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        return path

    def test_scans_json_and_jsonc_nodes_in_stable_order(self) -> None:
        self._write(
            "resource/pipeline/b.json",
            json.dumps(
                {
                    "RouteB": {
                        "desc": "四号谷地参考路线",
                        "custom_action": "MapNavigateAction",
                        "custom_action_param": {"path": [[5, 6]]},
                    },
                    "OtherAction": {
                        "custom_action": "OtherAction",
                        "custom_action_param": {"path": [[7, 8]]},
                    },
                    "AssertB": {
                        "desc": "四号谷地位置断言",
                        "recognition": "Custom",
                        "custom_recognition": "MapLocateAssertLocation",
                        "custom_recognition_param": {
                            "zone_id": "ValleyIV_Base",
                            "target": [10, 20, 30, 40],
                        },
                    },
                }
            ),
        )
        self._write(
            "resource/pipeline/a.jsonc",
            """
            {
                // 项目路线允许 JSONC 注释与尾逗号
                "RouteA": {
                    "desc": "武陵参考路线",
                    "custom_action": "MapNavigateAction",
                    "custom_action_param": {
                        "path": [
                            {"action": "ZONE", "zone_id": "Wuling_Base"},
                            [1, 2],
                            {"action": "NAVMESH", "target": [3, 4]},
                        ],
                    },
                },
                "MissingPath": {
                    "custom_action": "MapNavigateAction",
                    "custom_action_param": {},
                },
            }
            """,
        )
        self._write(
            "resource/pipeline/broken.json",
            '{"Broken": {"custom_action": "MapNavigateAction", ',
        )

        nodes = scan_project_import_nodes(self.assets_dir)

        self.assertEqual(
            [
                (
                    node.kind,
                    node.resource_path,
                    node.node_name,
                    node.desc,
                    node.point_count,
                    node.navmesh_count,
                    node.zone_ids,
                    node.zone_id,
                    node.target,
                )
                for node in nodes
            ],
            [
                (
                    "path",
                    "assets/resource/pipeline/a.jsonc",
                    "RouteA",
                    "武陵参考路线",
                    2,
                    1,
                    ("Wuling_Base",),
                    "",
                    None,
                ),
                (
                    "assert",
                    "assets/resource/pipeline/b.json",
                    "AssertB",
                    "四号谷地位置断言",
                    0,
                    0,
                    (),
                    "ValleyIV_Base",
                    (10.0, 20.0, 30.0, 40.0),
                ),
                (
                    "path",
                    "assets/resource/pipeline/b.json",
                    "RouteB",
                    "四号谷地参考路线",
                    1,
                    0,
                    (),
                    "",
                    None,
                ),
            ],
        )

    def test_loads_only_the_selected_map_navigate_path(self) -> None:
        expected_path = [[1, 2], [3, 4, "INTERACT"]]
        self._write(
            "resource/pipeline/routes.json",
            json.dumps(
                {
                    "First": {
                        "custom_action": "MapNavigateAction",
                        "custom_action_param": {"path": [[9, 9]]},
                    },
                    "Selected": {
                        "custom_action": "MapNavigateAction",
                        "custom_action_param": {"path": expected_path},
                    },
                }
            ),
        )

        loaded = load_project_import_node(
            "path",
            "assets/resource/pipeline/routes.json",
            "Selected",
            self.assets_dir,
        )

        self.assertEqual(
            loaded,
            {"kind": "path", "path": expected_path, "zip_enabled": False},
        )

    def test_preserves_selected_map_navigate_zip_option(self) -> None:
        expected_path = [[1, 2], [3, 4]]
        self._write(
            "resource/pipeline/routes.json",
            json.dumps(
                {
                    "Selected": {
                        "custom_action": "MapNavigateAction",
                        "custom_action_param": {"path": expected_path, "zip": True},
                    }
                }
            ),
        )

        loaded = load_project_import_node(
            "path",
            "assets/resource/pipeline/routes.json",
            "Selected",
            self.assets_dir,
        )

        self.assertEqual(
            loaded,
            {"kind": "path", "path": expected_path, "zip_enabled": True},
        )

    def test_loads_the_selected_assert_location(self) -> None:
        self._write(
            "resource/pipeline/assert.json",
            json.dumps(
                {
                    "SelectedAssert": {
                        "recognition": "Custom",
                        "custom_recognition": "MapLocateAssertLocation",
                        "custom_recognition_param": {
                            "zone_id": "Wuling_Base",
                            "target": [1, 2, 30, 40],
                        },
                    }
                }
            ),
        )

        loaded = load_project_import_node(
            "assert",
            "assets/resource/pipeline/assert.json",
            "SelectedAssert",
            self.assets_dir,
        )

        self.assertEqual(
            loaded,
            {
                "kind": "assert",
                "zone_id": "Wuling_Base",
                "target": [1.0, 2.0, 30.0, 40.0],
                "condition_count": 1,
            },
        )

    def test_rejects_paths_outside_assets(self) -> None:
        with self.assertRaisesRegex(ValueError, "不在项目 assets 目录内"):
            load_project_import_node(
                "path",
                "outside.json",
                "Route",
                self.assets_dir,
            )


if __name__ == "__main__":
    unittest.main()
