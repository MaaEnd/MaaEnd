"""按预先写好的步骤流程逐屏截图。"""

from .flow import Flow, FlowError, load_flow
from .node_names import NodeName, Nodes
from .types import Box, Capture, Point

__all__ = ["Box", "Capture", "Flow", "FlowError", "NodeName", "Nodes", "Point", "load_flow"]
