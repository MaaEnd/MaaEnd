from __future__ import annotations

import asyncio
import unittest
from types import SimpleNamespace
from unittest.mock import Mock, patch

from connection_models import RecordingSessionConfig

from web import serve


class ServerLifecycleTest(unittest.TestCase):
    def test_linux_locate_loads_base_resource_before_overlay(self) -> None:
        detail = {"status": 0, "mapName": "Wuling_Base", "x": 10, "y": 20, "rot": 45}
        session = Mock()
        session.tasker.get_latest_node.return_value = SimpleNamespace(
            recognition=SimpleNamespace(best_result=SimpleNamespace(detail=detail))
        )
        with (
            patch.object(serve, "AgentSession", return_value=session),
            patch.object(serve.time, "sleep"),
        ):
            result = serve.do_locate_once(SimpleNamespace(), RecordingSessionConfig(kind="linux"))
        self.assertTrue(result["ok"])
        self.assertEqual(session.open.call_args.kwargs.get("resource_dirs"), [serve.RESOURCE_DIR])
        session.close.assert_called_once_with()

    def test_lifespan_closes_navmesh_backend(self) -> None:
        async def run_lifespan() -> None:
            async with serve.lifespan(None):
                ensure_loading.assert_called_once_with()
                close.assert_not_called()

        with (
            patch.object(serve.navmesh_backend, "ensure_loading") as ensure_loading,
            patch.object(serve.navmesh_backend, "close") as close,
        ):
            asyncio.run(run_lifespan())

        close.assert_called_once_with()


if __name__ == "__main__":
    unittest.main()
