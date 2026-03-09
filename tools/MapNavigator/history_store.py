from __future__ import annotations

import copy
from typing import Generic, TypeVar


T = TypeVar("T")


class UndoRedoHistory(Generic[T]):
    """通用撤销/重做栈，按快照模式保存状态。"""

    def __init__(self, max_depth: int = 50) -> None:
        self._max_depth = max_depth
        self._undo_stack: list[T] = []
        self._redo_stack: list[T] = []

    def snapshot(self, value: T) -> None:
        """推入当前状态快照，并清空重做栈。"""
        self._undo_stack.append(copy.deepcopy(value))
        if len(self._undo_stack) > self._max_depth:
            self._undo_stack.pop(0)
        self._redo_stack.clear()

    def undo(self, current_value: T) -> T | None:
        if not self._undo_stack:
            return None
        self._redo_stack.append(copy.deepcopy(current_value))
        return self._undo_stack.pop()

    def redo(self, current_value: T) -> T | None:
        if not self._redo_stack:
            return None
        self._undo_stack.append(copy.deepcopy(current_value))
        return self._redo_stack.pop()

    def clear(self) -> None:
        self._undo_stack.clear()
        self._redo_stack.clear()
