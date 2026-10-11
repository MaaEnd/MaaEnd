from __future__ import annotations

import threading
import unittest
from types import SimpleNamespace
from typing import Any

from navtest_service import NODE_NAME, NavTestService
from runtime import INSTALL_DIR


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
            on_phase=lambda _phase, _text: None,
            on_ready=lambda: None,
            on_armed=lambda _count, _kind: None,
            on_run_state=lambda _running: None,
            on_position=lambda _position: None,
            on_finished=lambda _succeeded, _reason, _kind: None,
            on_error=lambda _message: None,
            on_closed=lambda: None,
        )

    def test_position_log_is_relative_to_agent_startup_directory(self) -> None:
        self.assertEqual(
            NavTestService._position_log_path(),
            INSTALL_DIR / "debug" / "cpp-algo" / "debug" / "maafw.log",
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
        self.assertEqual(param, {"path": path, "heading_source": "character", "zip": True})

    def test_heading_source_is_replaced_for_each_run(self) -> None:
        service = self._make_service()
        service._start_position_observer = lambda: None  # type: ignore[method-assign]
        resource = _Resource()
        tasker = SimpleNamespace(
            stopping=False,
            running=False,
            post_task=lambda _name: SimpleNamespace(wait=lambda: SimpleNamespace(succeeded=True)),
        )
        path = [{"action": "NAVMESH", "target": [10, 20]}]
        for heading_source in ("camera", "character"):
            with self.subTest(heading_source=heading_source):
                service.apply_client_message(
                    {"type": "arm", "path": path, "exported": True, "heading_source": heading_source}
                )
                service._run_once(tasker, resource)
                assert resource.override is not None
                self.assertEqual(
                    resource.override[NODE_NAME]["custom_action_param"],
                    {"path": path, "heading_source": heading_source},
                )

    def test_invalid_heading_source_does_not_replace_armed_route(self) -> None:
        service = self._make_service()
        service.arm([[1, 2]], exported=True, heading_source="camera")
        with self.assertRaises(ValueError):
            service.arm([[3, 4]], exported=True, heading_source="invalid")
        self.assertEqual(service._armed_path, [[1, 2]])
        self.assertEqual(service._armed_heading_source, "camera")

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
