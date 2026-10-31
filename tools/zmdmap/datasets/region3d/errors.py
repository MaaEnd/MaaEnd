"""地区总览的错误：按「读输入 / 看结构 / 算结果」三步各一种。

调用方（``Region3d.build``）只认基类 :class:`Region3dError`，统一折成数据集错误；消息里要写清哪一步、
哪份输入出了问题，不要只丢一句「失败了」。
"""


class Region3dError(Exception):
    """这份地区总览生成不出来。"""


class InputError(Region3dError):
    """当版输入读不到、格式不认识，或表、Lua、prefab 之间互相矛盾。"""


class LayoutError(Region3dError):
    """prefab 的结构超出了本生成器认识的范围：父链、按钮、样式或相机对不上。"""


class GenerationError(Region3dError):
    """结构都认识，但算不出结果：投影越界、点击点无解之类。"""
