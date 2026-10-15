from __future__ import annotations

import unittest
from types import SimpleNamespace
from unittest.mock import Mock, patch

import connectors
from connection_models import LinuxConnectionConfig
from connectors import LinuxRecordingConnector


class LinuxRecordingConnectorTest(unittest.TestCase):
    def test_enables_win32_key_translation_for_navigation(self) -> None:
        controller_type = Mock()
        connector = LinuxRecordingConnector(
            SimpleNamespace(LinuxController=controller_type),
            LinuxConnectionConfig(pw_node_id=42, eis_socket_path="/tmp/test-eis"),
        )

        controller = connector.connect()

        controller_type.assert_called_once_with(
            {
                "screencap_method": 4,
                "input_method": 4,
                "pw_node_id": 42,
                "eis_socket_path": "/tmp/test-eis",
                "use_win32_vk_code": True,
            }
        )
        controller.post_connection.return_value.wait.assert_called_once_with()

    def test_loads_linux_overlay_with_current_resource_api(self) -> None:
        job = Mock(succeeded=True)
        resource = SimpleNamespace(post_bundle=Mock(return_value=job))
        connector = LinuxRecordingConnector(SimpleNamespace(), LinuxConnectionConfig())

        with patch.object(connectors, "RESOURCE_LINUX_DIR") as resource_dir:
            resource_dir.exists.return_value = True
            connector.attach_resource(resource)
            resource.post_bundle.assert_called_once_with(str(resource_dir))
        job.wait.assert_called_once_with()

    def test_reports_linux_overlay_load_failure(self) -> None:
        resource = SimpleNamespace(post_bundle=Mock(return_value=Mock(succeeded=False)))
        connector = LinuxRecordingConnector(SimpleNamespace(), LinuxConnectionConfig())

        with patch.object(connectors, "RESOURCE_LINUX_DIR") as resource_dir:
            resource_dir.exists.return_value = True
            with self.assertRaisesRegex(RuntimeError, "Linux"):
                connector.attach_resource(resource)

    def test_skips_missing_linux_overlay(self) -> None:
        resource = SimpleNamespace(post_bundle=Mock())
        connector = LinuxRecordingConnector(SimpleNamespace(), LinuxConnectionConfig())

        with patch.object(connectors, "RESOURCE_LINUX_DIR") as resource_dir:
            resource_dir.exists.return_value = False
            connector.attach_resource(resource)
        resource.post_bundle.assert_not_called()


if __name__ == "__main__":
    unittest.main()
