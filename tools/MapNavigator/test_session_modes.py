from __future__ import annotations

import sys
import unittest
from types import ModuleType
from unittest.mock import Mock, patch

from session_modes import _build_navtest


class SessionModesTest(unittest.TestCase):
    def test_initial_navtest_arm_preserves_zipline_setting(self) -> None:
        connection_models = ModuleType("connection_models")
        session_config = object()
        connection_models.session_config_from_payload = Mock(return_value=session_config)  # type: ignore[attr-defined]

        navtest_service = ModuleType("navtest_service")
        service = Mock()
        service_type = Mock(return_value=service)
        navtest_service.NavTestService = service_type  # type: ignore[attr-defined]

        path = [{"action": "NAVMESH", "target": [10, 20]}]
        start = {
            "path": path,
            "exported": True,
            "zip": True,
            "config": {"kind": "win32"},
        }
        with patch.dict(
            sys.modules,
            {
                "connection_models": connection_models,
                "navtest_service": navtest_service,
            },
        ):
            built = _build_navtest(object(), Mock(), Mock(), start)

        self.assertIs(built, service)
        service.arm.assert_called_once_with(
            path,
            exported=True,
            zip_enabled=True,
            assert_target=None,
        )
        service.start.assert_called_once_with(session_config)


if __name__ == "__main__":
    unittest.main()
