from __future__ import annotations

import asyncio
import unittest
from unittest.mock import AsyncMock, patch

from web import serve


class ServerLifecycleTest(unittest.TestCase):
    def test_lifespan_closes_navmesh_backend(self) -> None:
        async def run_lifespan() -> None:
            async with serve.lifespan(None):
                ensure_loading.assert_called_once_with()
                close.assert_not_awaited()

        with (
            patch.object(serve.navmesh_backend, "ensure_loading") as ensure_loading,
            patch.object(serve.navmesh_backend, "close", new_callable=AsyncMock) as close,
        ):
            asyncio.run(run_lifespan())

        close.assert_awaited_once_with()


if __name__ == "__main__":
    unittest.main()
