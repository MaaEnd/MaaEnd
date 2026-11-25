"""流程里填节点名的写法：Nodes.节点名。

运行时 Nodes.XXX 就是字符串 "XXX"，NodeName 就是 str。同目录的 node_names.pyi 是生成出来的类型存根：
把资源里每个节点写成 Nodes 的一个属性，属性下面挂着节点的说明（desc、节点上方的 // 注释、
识别和动作类型、定义在哪个文件）。编辑器（VS Code 的 Pylance、pyright）读到它，
写 Nodes. 时就会列出节点名并显示说明，写错的名字当场标红。

存根不进仓库，用 uv run test-capture nodes 生成；跑 run / diagram 也会顺手刷新。
没生成时编辑器只知道 Nodes.XXX 是字符串，什么都不报，也不补全。
"""

NodeName = str


class _NodesMeta(type):
    """取 Nodes 上任何属性都直接返回属性名本身。"""

    def __getattr__(cls, name: str) -> str:
        if name.startswith("__") and name.endswith("__"):
            raise AttributeError(name)
        return name


class Nodes(metaclass=_NodesMeta):
    """每个 pipeline 节点一个属性，值就是节点名。"""
