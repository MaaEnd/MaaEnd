from __future__ import annotations

import time
import unittest
from types import SimpleNamespace

from connection_models import RecordingSessionConfig
from recording_service import RecordingService, parse_live_position


def _tasker_with_detail(detail: dict) -> SimpleNamespace:
    result = SimpleNamespace(detail=detail)
    recognition = SimpleNamespace(best_result=result)
    node = SimpleNamespace(recognition=recognition)
    return SimpleNamespace(get_latest_node=lambda _name: node)


class ParseLivePositionTest(unittest.TestCase):
    def test_parses_position_and_normalizes_heading(self) -> None:
        position = parse_live_position(
            {
                "status": 0,
                "mapName": "Wuling_Base",
                "x": 123.5,
                "y": 456,
                "rot": 361.25,
            }
        )

        self.assertIsNotNone(position)
        assert position is not None
        self.assertEqual(position.zone, "Wuling_Base")
        self.assertEqual((position.x, position.y), (123.5, 456.0))
        self.assertEqual(position.rot, 1.25)
        self.assertTrue(position.valid)

    def test_keeps_position_when_heading_is_unavailable(self) -> None:
        position = parse_live_position({"status": 0, "mapName": "Wuling_Base", "x": 1, "y": 2})

        self.assertIsNotNone(position)
        assert position is not None
        self.assertIsNone(position.rot)

    def test_rejects_failed_or_non_finite_position(self) -> None:
        self.assertIsNone(parse_live_position({"status": 1, "mapName": "Wuling_Base", "x": 1, "y": 2}))
        self.assertIsNone(
            parse_live_position({"status": 0, "mapName": "Wuling_Base", "x": float("nan"), "y": 2})
        )


class RecordingServicePositionTest(unittest.TestCase):
    def test_updates_snapshot_and_throttles_ui_events(self) -> None:
        positions = []
        statuses = []
        service = RecordingService(
            runtime=SimpleNamespace(),
            on_status=lambda text, color: statuses.append((text, color)),
            on_finished=lambda _points: None,
            on_error=lambda _message: None,
            on_live_position=positions.append,
        )
        service._session_config = RecordingSessionConfig(kind="win32")

        first = {"status": 0, "mapName": "Wuling_Base", "x": 10, "y": 20, "rot": 45}
        service._consume_latest_result(_tasker_with_detail(first))
        service._consume_latest_result(_tasker_with_detail({**first, "x": 11, "rot": 90}))

        self.assertEqual(len(positions), 1)
        self.assertEqual(service.live_position.x, 11.0)
        self.assertEqual(service.live_position.rot, 90.0)
        self.assertEqual(len(statuses), 1)

        service._last_live_position_emit_at = time.monotonic() - service.LIVE_POSITION_EMIT_INTERVAL_SECONDS
        service._consume_latest_result(_tasker_with_detail({**first, "x": 12, "rot": 180}))

        self.assertEqual(len(positions), 2)
        self.assertEqual(positions[-1].rot, 180.0)


if __name__ == "__main__":
    unittest.main()
