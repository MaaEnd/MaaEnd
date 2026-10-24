"""把 MXU 预设实例（config/mxu-MaaEnd.json）解析成「任务 -> entry + pipeline_override」。

MXU 在 GUI 里做的事就是：读任务的 optionValues -> 按 option.cases 的 pipeline_override
合并成一个覆盖字典 -> 提交任务时传给框架。Linux 上没有 GUI，只能自己复现。

支持四种选项类型：
  switch   : {"type":"switch","value":bool}      -> case "Yes"/"No"
  checkbox : {"type":"checkbox","caseNames":[]}  -> 多个 case 依次合并
  select   : {"type":"select","caseName":str}    -> 单个 case
  input    : {"type":"input","values":{name:v}}  -> option 根上的 pipeline_override，{InputName} 占位

占位符替换规则（上游 interface.schema.json 的 inputItem.pipeline_type）：
  * 整串占位符（值就是 "{Name}"）——按 pipeline_type 转成 int/bool，保持 JSON 类型；
  * 嵌入字符串（"{Name}>{Other}"）——只替换文本，保持字符串。
取值顺序：optionValues.values[name] -> inputs[].default -> 缺失则整条 po 不合并
（宁可让静态 pipeline 的默认值生效，也不要写入一个解析不了的占位符）。

2026-10-20 修复记录：
  1. 原实现只展开两层，case.option 的子选项只登记不处理；上游把 AutoStockStaple 的
     购买清单下沉到第三层（*Items）后，28 条 attach 被静默丢弃 → 进商店滑完列表一件不买。
  2. 原实现完全忽略 input 型（po 在 option 根上），库存阈值/价格/最大次数一律没传。
  3. 类型转换：input 的整串占位符此前会被写成字符串（"4600"），go-service 的 int 字段
     反序列化失败即 return false —— 会把原本正常的任务跑挂。
"""

from __future__ import annotations

import glob
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from introspect_tasks import load_jsonc

# 必须指向当前部署目录（~/maaend）。此前默认回落到 Windows 安装目录，
# 导致直跑 run_daily_linux.py 时读到的是另一份 config/interface.json，
# 自有任务（WebStartGame）被判成「未知任务」。
ROOT = (
    os.environ.get("MAAEND_ROOT")
    or os.environ.get("MAAEND_DIST")
    or os.path.expanduser("~/maaend")
)
CONFIG = os.path.join(ROOT, "config", "mxu-MaaEnd.json")

MAX_DEPTH = 12
_PLACEHOLDER = re.compile(r"\{([A-Za-z0-9_]+)\}")


def deep_merge(dst, src):
    """dict 递归合并；其余（含列表）整体替换——与 pipeline_override 的语义一致。"""
    for k, v in src.items():
        if isinstance(v, dict) and isinstance(dst.get(k), dict):
            deep_merge(dst[k], v)
        else:
            dst[k] = v
    return dst


def load_all():
    iface = load_jsonc(os.path.join(ROOT, "interface.json"))
    tasks, options = {}, {}
    for pat in iface.get("import") or []:
        p = pat.replace(chr(92), "/")
        if not os.path.isabs(p):
            p = os.path.join(ROOT, p)
        for f in glob.glob(p):
            try:
                d = load_jsonc(f)
            except Exception:
                continue
            for k in ("task", "tasks"):
                for t in d.get(k) or []:
                    if isinstance(t, dict) and t.get("name"):
                        tasks[t["name"]] = t
            if isinstance(d.get("option"), dict):
                options.update(d["option"])
    return tasks, options


def _typed(raw, ptype):
    """按 inputItem.pipeline_type 转换整串占位符的值。"""
    s = str(raw).strip()
    if ptype == "int":
        try:
            return int(s)
        except ValueError:
            return str(raw)
    if ptype == "bool":
        return s.lower() in ("1", "true", "yes", "on")
    if ptype == "float":
        try:
            return float(s)
        except ValueError:
            return str(raw)
    return str(raw)


def render(node, mapping):
    """结构化替换：不经过 JSON 文本往返，因此值里的引号/反斜杠不会破坏结构。"""
    if isinstance(node, dict):
        return {k: render(v, mapping) for k, v in node.items()}
    if isinstance(node, list):
        return [render(v, mapping) for v in node]
    if isinstance(node, str):
        m = _PLACEHOLDER.fullmatch(node)
        if m:
            name = m.group(1)
            if name in mapping:
                return _typed(*mapping[name])
            return node  # 未解析的占位符原样保留（调用方决定是否跳过）
        out = node
        for name, (raw, _pt) in mapping.items():
            out = out.replace("{%s}" % name, str(raw))
        return out
    return node


def _render_value(node, raw):
    """把旧式 $value 占位符替换成给定字符串（保持字符串类型）。"""
    if isinstance(node, dict):
        return {k: _render_value(v, raw) for k, v in node.items()}
    if isinstance(node, list):
        return [_render_value(v, raw) for v in node]
    if isinstance(node, str):
        if node == "$value":
            return raw
        return node.replace("$value", raw)
    return node


def case_names_of(opt_key, value, options):
    """某个选项当前选中的 case 名列表。缺失时回落 option.default_case（与 MXU 一致）。"""
    o = options.get(opt_key) or {}
    cases = o.get("cases") or []
    names = [c.get("name") for c in cases]
    v = value or {}
    t = v.get("type") or o.get("type")
    default = o.get("default_case")

    if t == "switch":
        if "value" not in v or v.get("value") is None:
            if default in names:
                return [default]
            return ["Yes"] if "Yes" in names else (names[:1] if names else [])
        return ["Yes" if v.get("value") else "No"]
    if t == "checkbox":
        picked = v.get("caseNames")
        if picked is None:
            picked = default if isinstance(default, list) else ([default] if default else [])
        return [n for n in picked if n in names]
    if t == "select":
        one = v.get("caseName") or default
        return [one] if one in names else []  # 未知 case 不再静默回落第一个
    if t in ("input", "hotkey"):
        return [default] if default in names else (names[:1] if names else [])
    # 未知类型：取默认
    if isinstance(default, str):
        return [default]
    if isinstance(default, list):
        return [n for n in default if n in names]
    return []


def resolve_task(task, options, values, seen=None, depth=0):
    """返回 (override_dict, 说明列表)。"""
    out = {}
    notes = []
    seen = seen or set()

    # 任务级 pipeline_override（schema 的 taskItem.pipeline_override）先落地，
    # 再由选项覆盖——选项是用户在 GUI 里的选择，优先级更高。
    if isinstance(task.get("pipeline_override"), dict):
        deep_merge(out, task["pipeline_override"])

    def walk_option(opt_key, depth):
        """递归展开一个选项：合并选中 case 的 pipeline_override，并继续下钻它的子选项。"""
        if opt_key in seen:
            return
        if depth > MAX_DEPTH:
            notes.append("TRUNCATED:%s" % opt_key)
            return
        seen.add(opt_key)
        o = options.get(opt_key) or {}
        val = values.get(opt_key) or {}
        otype = o.get("type")

        # input / hotkey：pipeline_override 在 option 根上，用 {InputName} 占位
        if otype in ("input", "hotkey") and isinstance(o.get("pipeline_override"), dict):
            po = o["pipeline_override"]
            mapping, missing = {}, []
            for inp in o.get("inputs") or []:
                nm = inp.get("name")
                raw = (val.get("values") or {}).get(nm)
                if raw is None or raw == "":
                    raw = inp.get("default")
                if raw is None:
                    missing.append(nm)
                    continue
                mapping[nm] = (str(raw), inp.get("pipeline_type") or "string")
            refs = set(_PLACEHOLDER.findall(json.dumps(po, ensure_ascii=False)))
            declared = [inp.get("name") for inp in (o.get("inputs") or []) if inp.get("name")]
            # 只有「声明为 input 但取不到值」才算缺失；其余 {X} 是运行时变量
            # （例如 {AutoStockCurrentStockBill}），必须原样保留。
            unresolved = sorted(n for n in refs if n in declared and n not in mapping)
            if unresolved:
                # 不合并：让静态 pipeline 的默认值生效，绝不写入字面量占位符
                notes.append("MISSING_INPUT:%s.%s" % (opt_key, ",".join(unresolved)))
                return
            merged = render(po, mapping)
            v = val.get("value")
            if v not in (None, ""):
                merged = _render_value(merged, str(v))
            deep_merge(out, merged)
            notes.append("%s=%s" % (opt_key, ",".join("%s=%s" % (k, v[0]) for k, v in mapping.items()) or "input"))
            return

        chosen = case_names_of(opt_key, val, options)
        notes.append("%s=%s" % (opt_key, ",".join(chosen) or "?"))
        if otype == "checkbox":
            picked = val.get("caseNames")
            if picked is None:
                d = o.get("default_case")
                picked = d if isinstance(d, list) else ([d] if d else [])
            unknown = [n for n in picked if n not in [c.get("name") for c in (o.get("cases") or [])]]
            if unknown:
                notes.append("UNKNOWN_CASE:%s.%s" % (opt_key, ",".join(unknown)))
        if otype == "select" and not chosen:
            notes.append("UNKNOWN_CASE:%s=%s" % (opt_key, val.get("caseName")))
        for cname in chosen:
            case = next((c for c in (o.get("cases") or []) if c.get("name") == cname), None)
            if not case:
                continue
            po = case.get("pipeline_override")
            if isinstance(po, dict):
                v = val.get("value")
                deep_merge(out, _render_value(po, str(v)) if v not in (None, "") else po)
            for sub in case.get("option") or []:
                walk_option(sub, depth + 1)

    for opt_key in task.get("option") or []:
        walk_option(opt_key, depth)
    return out, notes


# 网页端自研任务：某些机器上 interface.json 没导入 tasks/web/WebTasks.json，
# 这里给一个等价映射兜底（entry 与接口里的一致）。
FALLBACK_ENTRY = {
    "WebStartGame": "WebStartGameEntry",
    "WebOpenMenu": "WebPressEsc",
    "WebCaptureProbe": "WebCaptureProbe",
}


def load_preset(instance_id_or_name):
    cfg = json.load(open(CONFIG, encoding="utf-8"))
    inst = None
    for i in cfg.get("instances") or []:
        if i.get("id") == instance_id_or_name or i.get("name") == instance_id_or_name:
            inst = i
            break
    if not inst:
        raise SystemExit("找不到实例: %s" % instance_id_or_name)
    tasks, options = load_all()
    plan = []
    for t in inst.get("tasks") or []:
        name = t.get("taskName")
        meta = tasks.get(name)
        if not meta:
            fb = FALLBACK_ENTRY.get(name)
            if fb:
                plan.append(
                    {
                        "name": name,
                        "entry": fb,
                        "enabled": t.get("enabled", True),
                        "n_override": 0,
                        "override": {},
                        "notes": ["内置回退"],
                    }
                )
            else:
                plan.append({"name": name, "entry": None, "error": "任务不存在于接口"})
            continue
        try:
            ov, notes = resolve_task(meta, options, t.get("optionValues") or {})
        except Exception as e:  # 单个任务解析失败不拖垮整个计划
            plan.append(
                {
                    "name": name,
                    "entry": meta.get("entry"),
                    "enabled": t.get("enabled", True),
                    "n_override": 0,
                    "override": {},
                    "notes": ["ERROR:%s" % e],
                }
            )
            continue
        plan.append(
            {
                "name": name,
                "entry": meta.get("entry"),
                "enabled": t.get("enabled", True),
                "n_override": len(ov),
                "override": ov,
                "notes": notes,
            }
        )
    return inst, plan


def main():
    iid = sys.argv[1] if len(sys.argv) > 1 else "hyasuml"
    inst, plan = load_preset(iid)
    print(
        "实例: %s (%s)  控制器=%s  资源=%s"
        % (inst.get("name"), inst.get("id"), inst.get("controllerName"), inst.get("resourceName"))
    )
    print("%-3s %-22s %-32s %-6s %s" % ("#", "任务", "entry", "覆盖项", "状态"))
    for i, p in enumerate(plan, 1):
        print(
            "%-3d %-22s %-32s %-6s %s"
            % (
                i,
                p["name"],
                str(p.get("entry")),
                p.get("n_override", "-"),
                p.get("error") or ("启用" if p.get("enabled") else "禁用"),
            )
        )
    if len(sys.argv) > 2 and sys.argv[2] == "--dump":
        for p in plan:
            print("\n==", p["name"])
            print(json.dumps(p.get("override"), ensure_ascii=False)[:600])


if __name__ == "__main__":
    main()
