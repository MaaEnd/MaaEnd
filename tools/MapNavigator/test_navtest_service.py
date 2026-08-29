from __future__ import annotations

import threading
import unittest
from types import SimpleNamespace
from typing import Any

from navtest_service import NODE_NAME, NavTestService


class _Resource:
    def __init__(self) -> None:
        self.override: dict[str, Any] | None = None

    def override_pipeline(self, override: dict[str, Any]) -> None:
        self.override = override


class NavTestServiceTest(unittest.TestCase):
    @staticmethod
    def _make_service() -> NavTestService:
        return NavTestService(
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

    def test_passes_zip_option_to_map_navigate_action(self) -> None:
        service = self._make_service()
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

    def test_waits_for_position_observer_before_posting_task(self) -> None:
        service = self._make_service()
        observer_started = threading.Event()
        release_observer = threading.Event()
        post_called = threading.Event()

        def observer_loop(_path: Any) -> None:
            observer_started.set()
            release_observer.wait(1.0)
            service._position_ready.set()
            service._position_stop.wait(1.0)

        service._position_observer_loop = observer_loop  # type: ignore[method-assign]
        service.arm([{"action": "NAVMESH", "target": [1083.307, 1455.27]}], exported=True)
        resource = _Resource()
        job = SimpleNamespace(succeeded=True)

        def post_task(_name: str) -> Any:
            post_called.set()
            return SimpleNamespace(wait=lambda: job)

        tasker = SimpleNamespace(stopping=False, running=False, post_task=post_task)
        worker = threading.Thread(target=service._run_once, args=(tasker, resource))
        worker.start()
        try:
            self.assertTrue(observer_started.wait(1.0))
            self.assertFalse(post_called.wait(0.05))
            release_observer.set()
            self.assertTrue(post_called.wait(1.0))
        finally:
            release_observer.set()
            worker.join(2.0)
        self.assertFalse(worker.is_alive())


if __name__ == "__main__":
    unittest.main()
