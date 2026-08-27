from __future__ import annotations

import unittest
from types import SimpleNamespace
from typing import Any

from navtest_service import NODE_NAME, NavTestService


class _Resource:
    def __init__(self) -> None:
        self.override: dict[str, Any] | None = None

    def override_pipeline(self, override: dict[str, Any]) -> None:
        self.override = override


class NavTestServiceZiplineTest(unittest.TestCase):
    def test_passes_zip_option_to_map_navigate_action(self) -> None:
        service = NavTestService(
            runtime=SimpleNamespace(),
            on_status=lambda _text, _color: None,
            on_ready=lambda: None,
            on_armed=lambda _count, _kind: None,
            on_run_state=lambda _running: None,
            on_position=lambda _position: None,
            on_finished=lambda _succeeded, _reason, _kind: None,
            on_error=lambda _message: None,
            on_closed=lambda: None,
        )
        resource = _Resource()
        job = SimpleNamespace(succeeded=True)
        tasker = SimpleNamespace(
            stopping=False,
            running=False,
            post_task=lambda _name: SimpleNamespace(wait=lambda: job),
        )
        path = [{"action": "NAVMESH", "target": [1083.307, 1455.27]}]

        service.arm(path, exported=True, zip_enabled=True)
        service._run_once(tasker, resource)

        self.assertIsNotNone(resource.override)
        assert resource.override is not None
        param = resource.override[NODE_NAME]["custom_action_param"]
        self.assertEqual(param, {"path": path, "zip": True})


if __name__ == "__main__":
    unittest.main()
