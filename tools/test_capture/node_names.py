"""流程里填节点名的参数类型。

运行时 NodeName 就是 str。同目录的 node_names.pyi 是生成出来的类型存根，
把资源里所有节点名列成 Literal：编辑器（VS Code 的 Pylance、pyright）读到它，
写 c.click("...") 时就能补全节点名，写错的名字当场标红。

存根不进仓库，用 uv run test-capture nodes 生成；跑 run / diagram 也会顺手刷新。
没生成时编辑器把 NodeName 当 str，什么都不报，也不补全。
"""

NodeName = str
