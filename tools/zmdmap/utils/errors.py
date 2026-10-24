"""共用的小异常。

``LocalDataError`` 只表示「本地这份数据读不出来，或形状不是预期的那样」：表格结构对不上、文件不存在、
文本表里没有这门语言。它继承 ``ValueError``，命令行那边按普通输入错误处理。
"""


class LocalDataError(ValueError):
    """本地数据缺失，或结构与预期不符。"""
