from __future__ import annotations

import threading
import unittest
from pathlib import Path
from typing import Any

from navmesh_backend import NavmeshBackend


class NavmeshBackendLatestQueryTest(unittest.TestCase):
    def test_discards_obsolete_waiting_queries_but_finishes_active_query(self) -> None:
        backend = NavmeshBackend(Path("base.nav.gz"))
        active_entered = threading.Event()
        release_active = threading.Event()
        calls: list[str] = []
        results: dict[str, dict[str, Any]] = {}

        backend._await_ready = lambda: None  # type: ignore[method-assign]

        def post_locked(_op: str, **params: Any) -> dict[str, Any]:
            request_id = str(params["request_id"])
            calls.append(request_id)
            if request_id == "active":
                active_entered.set()
                self.assertTrue(release_active.wait(2.0))
            return {"ok": True, "request_id": request_id}

        backend._post_locked = post_locked  # type: ignore[method-assign]

        def query(request_id: str) -> None:
            results[request_id] = backend.query_latest(
                "route-preview",
                "route_preview",
                request_id=request_id,
            )

        active = threading.Thread(target=query, args=("active",))
        obsolete = threading.Thread(target=query, args=("obsolete",))
        latest = threading.Thread(target=query, args=("latest",))

        active.start()
        self.assertTrue(active_entered.wait(2.0))
        obsolete.start()
        self.assertTrue(self._wait_for_generation(backend, "route-preview", 2))
        latest.start()

        self.assertTrue(
            self._wait_for_generation(backend, "route-preview", 3),
            "waiting queries did not register in time",
        )
        release_active.set()

        for thread in (active, obsolete, latest):
            thread.join(2.0)
            self.assertFalse(thread.is_alive())

        self.assertEqual(calls, ["active", "latest"])
        self.assertEqual(results["active"], {"ok": True, "request_id": "active"})
        self.assertEqual(results["obsolete"], {"ok": False, "stale": True})
        self.assertEqual(results["latest"], {"ok": True, "request_id": "latest"})

    @staticmethod
    def _wait_for_generation(backend: NavmeshBackend, key: str, expected: int) -> bool:
        for _ in range(200):
            with backend._latest_lock:
                if backend._latest_generation.get(key) == expected:
                    return True
            threading.Event().wait(0.005)
        return False


if __name__ == "__main__":
    unittest.main()
