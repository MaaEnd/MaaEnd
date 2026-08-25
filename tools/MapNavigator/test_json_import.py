from __future__ import annotations

import unittest

from json_import import discover_path_routes, export_path_nodes
from model import ActionType, PathPoint


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


if __name__ == "__main__":
    unittest.main()
