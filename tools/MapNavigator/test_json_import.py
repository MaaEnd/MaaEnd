from __future__ import annotations

import unittest

from json_import import export_path_nodes
from model import ActionType, PathPoint


def make_point(action: ActionType, *, strict: bool = False) -> PathPoint:
    return {
        "x": 1242.04,
        "y": 773.41,
        "action": int(action),
        "actions": [int(action)],
        "zone": "",
        "strict": strict,
    }


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


if __name__ == "__main__":
    unittest.main()
