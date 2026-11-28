"""把 capture_flow 画成一张 HTML：每步只出现一次，平台的运行顺序单独画成一张表。"""

from __future__ import annotations

import ast
import contextlib
import io
import itertools
import tokenize
from dataclasses import dataclass, field
from html import escape
from pathlib import Path

from .flow import Flow, RouteFunc, Step
from .nodes import Entry, Prepared, Run, chain, load_next_map, plan, prepare
from .runner import Segment, split_segments

# 每种调用的短名字和鼠标悬停时的解释。
KINDS: dict[str, tuple[str, str]] = {
    "run": ("跑", "按节点自己的识别和动作跑一次，不跟它的 next"),
    "click": ("点", "等节点命中，点它识别框的中心"),
    "wait": ("等", "等节点命中，不点也不动"),
    "hit": ("看", "看一眼现在命不命中，不等"),
    "text": ("读字", "等节点命中，取它 OCR 认出的文字"),
    "sub_task": ("整串", "跑节点并顺着 next 一路走完"),
    "goto": ("转到", "不在这个界面就先转过去"),
    "shot": ("截图", "等画面稳定后截一张"),
    "shot_when": ("截图", "等节点命中、画面稳定后截一张"),
    "click_at": ("点坐标", "点固定坐标（1280x720 基准）"),
    "swipe": ("滑动", "从一点滑到另一点"),
    "sleep": ("硬等", "固定等几秒"),
}

# 平台配色，按 --controller 给的顺序取。
PALETTE = ["#2563eb", "#16a34a", "#d97706", "#9333ea", "#0891b2", "#dc2626", "#65a30d", "#db2777"]
NEUTRAL = "#64748b"


@dataclass
class Comment:
    text: str
    # 独占一行的注释挂在下一条调用上面；写在代码后面的挂在那条调用后面。
    standalone: bool


@dataclass
class StepView:
    """一步在页面上要展示的全部内容。"""

    index: int
    step: Step
    segment: int
    doc: str
    entries: list[Entry]
    comments: dict[int, Comment]
    body: tuple[int, int]
    ran: list[str] = field(default_factory=list[str])
    skipped: list[str] = field(default_factory=list[str])


def read_comments(source: Path) -> dict[int, Comment]:
    """整份文件的注释，按行号索引。"""
    text = source.read_text(encoding="utf-8")
    found: dict[int, Comment] = {}
    lines = text.splitlines()
    for token in tokenize.generate_tokens(io.StringIO(text).readline):
        if token.type != tokenize.COMMENT:
            continue
        line = token.start[0]
        body = token.string.lstrip("#").strip()
        if body and not set(body) <= set("-=# "):
            found[line] = Comment(body, lines[line - 1].strip().startswith("#"))
    return found


def function_body(function: ast.FunctionDef | None) -> tuple[int, int]:
    """函数体的起止行：从 def 下一行算起，函数开头独占一行的注释也算在里面。"""
    if function is None or function.end_lineno is None:
        return (0, -1)
    return (function.lineno + 1, function.end_lineno)


def kind_tag(kind: str) -> str:
    label, tip = KINDS.get(kind, (kind, ""))
    return f'<span class="tag k-{kind}" title="{escape(tip)}">{escape(label)}</span>'


def chip(name: str, color: str, extra: str = "", tip: str = "") -> str:
    title = f' title="{escape(tip)}"' if tip else ""
    return f'<span class="chip {extra}" style="--c:{color}"{title}>{escape(name)}</span>'


def entry_html(entry: Entry, inline: str, notes: list[str], chains: dict[str, list[str]]) -> str:
    """一条调用。截图单独做成醒目的一行，其余是「标签 + 节点名」。"""
    note_html = "".join(f'<div class="note">{escape(text)}</div>' for text in notes)
    where = f'<span class="where">{escape(entry.where)}</span>' if entry.where else ""
    tail = f'<span class="said">{escape(inline)}</span>' if inline else ""
    if entry.kind in ("shot", "shot_when"):
        shot_name = entry.shot if entry.kind == "shot_when" else entry.name
        cause = (
            f'<span class="after">等 <code>{escape(entry.name)}</code> 出现后</span>'
            if entry.kind == "shot_when"
            else '<span class="after">画面稳定后</span>'
        )
        return (
            f'<li class="call shot">{note_html}<div class="line">'
            f'<span class="cam" aria-hidden="true"></span><b class="shot-name">{escape(shot_name or "（没写名字）")}</b>'
            f"{cause}{where}{tail}</div></li>"
        )
    code_class = "plain" if entry.kind in ("click_at", "swipe", "sleep") else ""
    extra = ""
    if entry.kind == "goto" and entry.route:
        extra = f'<span class="after">不在就跑 <code>{escape(entry.route)}</code></span>'
    elif entry.kind == "sub_task" and entry.name in chains:
        extra = chain_html(chains[entry.name])
    return (
        f'<li class="call">{note_html}<div class="line">{kind_tag(entry.kind)}'
        f'<code class="{code_class}">{escape(entry.name)}</code>{extra}{where}{tail}</div></li>'
    )


def chain_html(nodes: list[str]) -> str:
    if len(nodes) <= 1:
        return ""
    items = "".join(f"<li><code>{escape(name)}</code></li>" for name in nodes)
    return (
        f'<details class="chain"><summary>顺着 next 会经过 {len(nodes)} 个节点</summary>'
        f"<ol>{items}</ol><p>只沿第一条 next 往下列，实际走哪条看当时界面。</p></details>"
    )


def calls_html(
    entries: list[Entry],
    comments: dict[int, Comment],
    body: tuple[int, int],
    chains: dict[str, list[str]],
) -> tuple[str, list[str]]:
    """按代码顺序排好的调用；返回 HTML 和没挂上任何调用的注释。"""
    used: set[int] = set()
    rows: list[str] = []
    previous = body[0] - 1
    for entry in entries:
        notes: list[str] = []
        for line in range(previous + 1, entry.line):
            comment = comments.get(line)
            if comment and comment.standalone:
                notes.append(comment.text)
                used.add(line)
        inline = ""
        same = comments.get(entry.line)
        if same and not same.standalone:
            inline = same.text
            used.add(entry.line)
        rows.append(entry_html(entry, inline, notes, chains))
        previous = max(previous, entry.line)
    loose = [
        comment.text for line, comment in sorted(comments.items()) if body[0] <= line <= body[1] and line not in used
    ]
    if not rows:
        return '<p class="empty">这一步没有调用任何节点</p>', loose
    return f'<ol class="calls">{"".join(rows)}</ol>', loose


def route_html(step: Step, prepared: Prepared, comments: dict[int, Comment], next_map: dict[str, list[str]]) -> str:
    """「要在哪个界面、不在时怎么过去」。"""
    enter = f"<code>{escape(step.enter)}</code>" if step.enter else '<span class="muted">没写，不检查界面</span>'
    route: str | RouteFunc | None = step.route
    if route is None:
        how = '回大世界 <code>SceneAnyEnterWorld</code>，<span class="muted">没写 route 时的默认做法</span>'
    elif isinstance(route, str):
        how = f"跑 <code>{escape(route)}</code>{chain_html(chain(next_map, route))}"
    else:
        name = getattr(route, "__name__", "流程函数")
        function = prepared.functions.get(name)
        doc = ast.get_docstring(function) if function is not None else None
        inner, _ = calls_html(prepared.entries.get(name, []), comments, function_body(function), {})
        doc_html = f'<p class="doc">{escape(doc)}</p>' if doc else ""
        how = f'<details class="route-fn"><summary>交给 <b>{escape(name)}</b> 处理</summary>{doc_html}{inner}</details>'
    return f'<dl class="where-box"><dt>要在的界面</dt><dd>{enter}</dd><dt>不在时</dt><dd>{how}</dd></dl>'


def step_html(
    view: StepView,
    prepared: Prepared,
    next_map: dict[str, list[str]],
    colors: dict[str, str],
    controllers: tuple[str, ...],
) -> str:
    step = view.step
    chains: dict[str, list[str]] = {
        entry.name: chain(next_map, entry.name) for entry in view.entries if entry.kind == "sub_task"
    }
    calls, loose = calls_html(view.entries, view.comments, view.body, chains)
    badges: list[str] = []
    if step.once:
        badges.append('<span class="badge once" title="第一个跑到的平台做完，别的平台跳过">只做一次</span>')
    shots = sum(1 for entry in view.entries if entry.kind in ("shot", "shot_when"))
    badges.append(f'<span class="badge shots">{shots} 张图</span>' if shots else '<span class="badge">不截图</span>')
    platforms = ""
    if controllers:
        items = [chip(name, colors[name]) for name in view.ran]
        items += [chip(name, colors[name], "off", "只做一次的步骤，前面的平台已经做过") for name in view.skipped]
        platforms = f'<div class="plats">{"".join(items)}</div>'
    doc = f'<p class="doc">{escape(view.doc)}</p>' if view.doc else ""
    loose_html = ""
    if loose:
        loose_html = '<ul class="loose">' + "".join(f"<li>{escape(text)}</li>" for text in loose) + "</ul>"
    return (
        f'<article class="step" id="step-{view.index}" data-once="{int(step.once)}" '
        f'data-platforms="{escape(" ".join(view.ran))}">'
        f'<header><span class="num">{view.index}</span><h3>{escape(step.name)}</h3>{"".join(badges)}{platforms}</header>'
        f"{doc}{route_html(step, prepared, view.comments, next_map)}{calls}{loose_html}"
        "</article>"
    )


def plan_html(
    segments: list[Segment],
    allocation: dict[int, list[Run]] | None,
    colors: dict[str, str],
    controllers: tuple[str, ...],
) -> str:
    """平台 × 段的表格，格子里写这台平台第几个上场、跑几步；下面一条是实际先后顺序。"""
    if not allocation:
        return (
            '<section class="panel plan"><h2>运行顺序</h2><p class="muted">'
            "没带 <code>--controller</code>：每段在所有平台上各跑一遍，平台先后一段正、一段反。"
            "带上平台名才画得出谁先跑、谁跳过、在哪里换平台。</p></section>"
        )
    order: list[tuple[int, Run]] = [
        (segment.index, run) for segment in segments for run in allocation.get(segment.index, []) if run.steps
    ]
    position = {(index, run.controller): number for number, (index, run) in enumerate(order, 1)}
    head = "".join(
        f"<th>第 {segment.index} 段<small>步骤 {segment.steps[0][0]}"
        f"{'–' + str(segment.steps[-1][0]) if len(segment.steps) > 1 else ''}</small></th>"
        for segment in segments
    )
    rows: list[str] = []
    for name in controllers:
        cells: list[str] = []
        for segment in segments:
            run = next((item for item in allocation.get(segment.index, []) if item.controller == name), None)
            if run is None or not run.steps:
                cells.append('<td class="skip">整段跳过</td>')
                continue
            note = f"跑 {len(run.steps)} 步"
            if run.skipped:
                note += f"，跳过 {len(run.skipped)} 步"
            cells.append(
                f'<td style="--c:{colors[name]}"><span class="order">{position[(segment.index, name)]}</span>'
                f"<small>{note}</small></td>"
            )
        rows.append(f"<tr><th>{chip(name, colors[name])}</th>{''.join(cells)}</tr>")
    switches = sum(1 for (_, a), (_, b) in itertools.pairwise(order) if a.controller != b.controller)
    strip: list[str] = []
    for number, (index, run) in enumerate(order):
        if number and order[number - 1][1].controller != run.controller:
            strip.append('<span class="swap" title="换平台：游戏不关，只换操作的那一端（会顶号）">⇄</span>')
        elif number:
            strip.append('<span class="next">→</span>')
        strip.append(chip(f"{run.controller} · 段{index}", colors[run.controller]))
    return (
        '<section class="panel plan"><h2>运行顺序</h2>'
        f"<table><thead><tr><th></th>{head}</tr></thead><tbody>{''.join(rows)}</tbody></table>"
        f'<div class="strip">{"".join(strip)}</div>'
        f'<p class="muted">格子里的数字是第几个上场；⇄ 是换平台（顶号），一共 {switches} 次。</p></section>'
    )


def render(flow: Flow, controllers: tuple[str, ...] = ()) -> str:
    segments = split_segments(flow)
    next_map = load_next_map(Path.cwd())
    prepared = prepare(flow.source, flow.steps) if flow.source is not None else Prepared()
    comments = read_comments(flow.source) if flow.source is not None else {}
    colors = {name: PALETTE[index % len(PALETTE)] for index, name in enumerate(controllers)}
    allocation = plan(segments, controllers) if controllers else None

    views: list[StepView] = []
    for segment in segments:
        runs = allocation.get(segment.index, []) if allocation else []
        for index, step in segment.steps:
            function = prepared.functions.get(step.func.__name__)
            views.append(
                StepView(
                    index=index,
                    step=step,
                    segment=segment.index,
                    doc=(ast.get_docstring(function) or "") if function is not None else "",
                    entries=prepared.entries.get(step.func.__name__, []),
                    comments=comments,
                    body=function_body(function),
                    ran=[run.controller for run in runs if step.name in run.steps],
                    skipped=[run.controller for run in runs if step.name in run.skipped],
                )
            )

    shots = [(view, entry) for view in views for entry in view.entries if entry.kind in ("shot", "shot_when")]
    node_count = len(
        {
            entry.name
            for view in views
            for entry in view.entries
            if entry.kind not in ("shot", "click_at", "swipe", "sleep")
        }
        | {entry.name for view in views for entry in view.entries if entry.kind == "shot_when"}
    )
    where = str(flow.source) if flow.source is not None else "（不是从文件加载的）"
    if flow.source is not None:
        with contextlib.suppress(ValueError):
            where = flow.source.resolve().relative_to(Path.cwd().resolve()).as_posix()
    module_doc = ""
    if flow.source is not None:
        module_doc = ast.get_docstring(ast.parse(flow.source.read_text(encoding="utf-8"))) or ""

    parts: list[str] = [HEAD.replace("{{TITLE}}", escape(flow.name))]
    parts.append('<div class="page"><header class="top">')
    parts.append(f'<p class="eyebrow">截图流程 · <code>{escape(where)}</code></p><h1>{escape(flow.name)}</h1>')
    if module_doc:
        lead, _, rest = module_doc.partition("\n\n")
        parts.append(f'<p class="lead">{escape(lead)}</p>')
        if rest.strip():
            parts.append(
                f'<details class="more"><summary>流程说明全文</summary><pre>{escape(rest.strip())}</pre></details>'
            )
    stats = [
        (len(flow.steps), "步"),
        (len(segments), "段"),
        (len(shots), "张图"),
        (sum(1 for step in flow.steps if step.once), "步只做一次"),
        (node_count, "个节点"),
    ]
    parts.append(
        '<div class="stats">'
        + "".join(f"<div><b>{value}</b><span>{label}</span></div>" for value, label in stats)
        + "</div>"
    )
    parts.append("</header>")

    parts.append(plan_html(segments, allocation, colors, controllers))
    if shots:
        items = "".join(
            f'<li><a href="#step-{view.index}">{escape(entry.shot if entry.kind == "shot_when" else entry.name)}</a>'
            f'<span class="muted">第 {view.index} 步</span></li>'
            for view, entry in shots
        )
        parts.append(
            f'<details class="panel gallery"><summary>会截的 {len(shots)} 张图</summary><ol>{items}</ol></details>'
        )
    parts.append(toolbar_html(controllers))

    parts.append("<main>")
    by_segment: dict[int, list[StepView]] = {}
    for view in views:
        by_segment.setdefault(view.segment, []).append(view)
    for segment in segments:
        members = by_segment.get(segment.index, [])
        runs = allocation.get(segment.index, []) if allocation else []
        order = [chip(run.controller, colors[run.controller]) for run in runs if run.steps]
        order_html = (
            '<span class="next">→</span>'.join(order) if order else '<span class="muted">每台平台各跑一遍</span>'
        )
        span = f"步骤 {members[0].index}" + (f"–{members[-1].index}" if len(members) > 1 else "") if members else ""
        parts.append(
            f'<section class="segment"><div class="seg-head"><h2>第 {segment.index} 段</h2>'
            f'<span class="muted">{span}</span><span class="seg-order">{order_html}</span></div>'
        )
        parts.extend(step_html(view, prepared, next_map, colors, controllers) for view in members)
        parts.append("</section>")
    parts.append("</main>")
    parts.append(LEGEND)
    parts.append("</div>")
    parts.append(SCRIPT)
    parts.append("</body></html>")
    return "\n".join(parts)


def toolbar_html(controllers: tuple[str, ...]) -> str:
    buttons = ['<button class="pick" data-platform="" aria-pressed="true">全部平台</button>']
    buttons += [f'<button class="pick" data-platform="{escape(name)}">{escape(name)}</button>' for name in controllers]
    buttons.append('<button class="pick" data-once="1" aria-pressed="false">只看只做一次的</button>')
    return '<nav class="toolbar">' + "".join(buttons) + "</nav>"


LEGEND = (
    '<footer class="legend">'
    + "".join(
        f"<span>{kind_tag(kind)}{escape(KINDS[kind][1])}</span>"
        for kind in ("run", "click", "wait", "hit", "sub_task", "goto", "click_at", "swipe", "sleep")
    )
    + '<span><span class="cam"></span>截一张图，粗体是图片名</span></footer>'
)


HEAD = """<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>截图流程 · {{TITLE}}</title>
<style>
:root {
  color-scheme: light dark;
  --bg: #f5f6f8; --card: #ffffff; --line: #e4e7ec; --soft: #f2f4f7;
  --text: #101828; --muted: #667085; --accent: #2563eb;
  --shot-bg: #fff4ed; --shot-line: #fdba8c; --shot-text: #9a3412;
  --once-bg: #fef3f2; --once-text: #b42318;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0f1115; --card: #171a21; --line: #2a2f3a; --soft: #20242d;
    --text: #e6e8ec; --muted: #98a2b3; --accent: #6ea8fe;
    --shot-bg: #2b1d14; --shot-line: #7c3f16; --shot-text: #fdba8c;
    --once-bg: #3a1a17; --once-text: #fda29b;
  }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--text);
       font: 14px/1.6 -apple-system, "Segoe UI", "PingFang SC", "Noto Sans CJK SC", "Microsoft YaHei", sans-serif; }
code { font: 12.5px/1.4 ui-monospace, "Cascadia Code", "JetBrains Mono", Consolas, monospace;
       background: var(--soft); border-radius: 5px; padding: 1px 6px; overflow-wrap: anywhere; }
code.plain { background: none; padding: 0; }
.muted { color: var(--muted); }
.page { max-width: 980px; margin: 0 auto; padding: 36px 20px 80px; }
.top h1 { margin: 4px 0 8px; font-size: 28px; letter-spacing: .5px; }
.eyebrow { margin: 0; color: var(--muted); font-size: 12px; }
.eyebrow code { background: none; padding: 0; color: var(--muted); }
.lead { margin: 0 0 12px; color: var(--muted); white-space: pre-line; }
.more summary, .gallery summary, .chain summary, .route-fn summary { cursor: pointer; color: var(--accent); }
.more { margin-bottom: 16px; font-size: 13px; }
.more pre { white-space: pre-wrap; font: inherit; color: var(--muted); margin: 8px 0 0;
            padding: 12px 14px; background: var(--card); border: 1px solid var(--line); border-radius: 10px; }
.stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 10px; margin: 8px 0 22px; }
.stats div { background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: 10px 14px; }
.stats b { display: block; font-size: 22px; line-height: 1.2; }
.stats span { font-size: 12px; color: var(--muted); }
.panel { background: var(--card); border: 1px solid var(--line); border-radius: 14px; padding: 16px 18px; margin-bottom: 14px; }
.panel h2 { margin: 0 0 10px; font-size: 15px; }
.panel p { margin: 10px 0 0; font-size: 12.5px; }
.plan table { width: 100%; border-collapse: separate; border-spacing: 6px; table-layout: fixed; margin: -6px; width: calc(100% + 12px); }
.plan th { font-weight: 600; font-size: 12.5px; text-align: center; }
.plan th small { display: block; font-weight: normal; color: var(--muted); }
.plan th:first-child { text-align: left; width: 130px; }
.plan td { text-align: center; border-radius: 10px; padding: 8px 4px; height: 58px;
           background: color-mix(in srgb, var(--c) 12%, var(--card)); border: 1px solid color-mix(in srgb, var(--c) 35%, var(--card)); }
.plan td.skip { background: repeating-linear-gradient(135deg, var(--soft) 0 6px, transparent 6px 12px);
                border: 1px dashed var(--line); color: var(--muted); font-size: 12px; }
.plan .order { display: inline-flex; width: 24px; height: 24px; border-radius: 50%; background: var(--c); color: #fff;
               align-items: center; justify-content: center; font-weight: 600; font-size: 12px; }
.plan td small { display: block; color: var(--muted); font-size: 11.5px; margin-top: 2px; }
.strip { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 12px; }
.next { color: var(--muted); }
.swap { color: #d97706; font-weight: 700; padding: 0 2px; }
.chip { display: inline-block; font-size: 12px; line-height: 20px; padding: 0 9px; border-radius: 999px; white-space: nowrap;
        color: var(--c); background: color-mix(in srgb, var(--c) 12%, var(--card)); border: 1px solid color-mix(in srgb, var(--c) 40%, var(--card)); }
.chip.off { color: var(--muted); background: none; border-style: dashed; border-color: var(--line); text-decoration: line-through; }
.gallery ol { margin: 10px 0 0; padding-left: 22px; columns: 2 260px; font-size: 13px; }
.gallery li { break-inside: avoid; }
.gallery a { color: var(--text); text-decoration: none; margin-right: 6px; }
.gallery a:hover { color: var(--accent); }
.toolbar { position: sticky; top: 0; z-index: 2; display: flex; flex-wrap: wrap; gap: 6px; padding: 10px 0; margin-bottom: 8px;
           background: color-mix(in srgb, var(--bg) 88%, transparent); backdrop-filter: blur(6px); }
.pick { font: inherit; font-size: 12.5px; padding: 4px 12px; border-radius: 999px; cursor: pointer;
        border: 1px solid var(--line); background: var(--card); color: var(--text); }
.pick[aria-pressed="true"] { background: var(--text); border-color: var(--text); color: var(--bg); }
.segment { position: relative; margin: 18px 0 0; padding-left: 18px; border-left: 2px solid var(--line); }
.seg-head { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin: 0 0 10px -27px; }
.seg-head h2 { margin: 0; font-size: 14px; background: var(--bg); padding: 2px 8px 2px 0; }
.seg-head h2::before { content: ""; display: inline-block; width: 12px; height: 12px; border-radius: 50%; margin-right: 8px;
                       background: var(--card); border: 3px solid var(--accent); vertical-align: -1px; }
.seg-order { margin-left: auto; display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.step { background: var(--card); border: 1px solid var(--line); border-radius: 14px; padding: 14px 18px 12px; margin-bottom: 12px;
        transition: opacity .15s; }
.step.dim { opacity: .22; }
.step header { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.num { flex: 0 0 auto; width: 26px; height: 26px; border-radius: 8px; background: var(--soft); color: var(--muted);
       display: inline-flex; align-items: center; justify-content: center; font-size: 12px; font-weight: 600; }
.step h3 { margin: 0; font-size: 16px; }
.badge { font-size: 11.5px; padding: 1px 8px; border-radius: 6px; background: var(--soft); color: var(--muted); }
.badge.once { background: var(--once-bg); color: var(--once-text); font-weight: 600; }
.badge.shots { background: var(--shot-bg); color: var(--shot-text); }
.plats { margin-left: auto; display: flex; gap: 4px; flex-wrap: wrap; }
.doc { margin: 8px 0 0; color: var(--muted); font-size: 13px; white-space: pre-line; }
.where-box { display: grid; grid-template-columns: auto 1fr; gap: 4px 12px; margin: 10px 0 0; padding: 10px 12px;
             background: var(--soft); border-radius: 10px; font-size: 13px; }
.where-box dt { color: var(--muted); }
.where-box dd { margin: 0; min-width: 0; }
.where-box code { background: var(--card); }
.route-fn .doc { margin: 6px 0 4px; }
.route-fn .calls { margin-top: 6px; }
.chain { display: inline-block; margin-left: 6px; font-size: 12px; }
.chain[open] { display: block; margin: 6px 0 0; }
.chain ol { margin: 6px 0 0; padding-left: 24px; columns: 2 240px; }
.chain p { margin: 4px 0 0; color: var(--muted); }
.calls { list-style: none; margin: 12px 0 0; padding: 0; counter-reset: call; }
.call { position: relative; padding: 3px 0 3px 30px; counter-increment: call; }
.call::before { content: counter(call); position: absolute; left: 0; top: 6px; width: 20px; text-align: right;
                font-size: 11px; color: var(--muted); }
.call .line { display: flex; flex-wrap: wrap; align-items: baseline; gap: 6px 8px; }
.call .note { color: var(--muted); font-size: 12.5px; margin: 4px 0 2px; }
.call .note::before { content: "# "; opacity: .6; }
.said { color: var(--muted); font-size: 12.5px; }
.said::before { content: "— "; opacity: .6; }
.where { font-size: 11px; padding: 0 6px; border-radius: 4px; border: 1px dashed var(--line); color: var(--muted); }
.after { color: var(--muted); font-size: 12.5px; }
.tag { flex: 0 0 auto; min-width: 34px; text-align: center; font-size: 11.5px; line-height: 20px; padding: 0 6px; border-radius: 5px;
       background: var(--soft); color: var(--muted); cursor: help; }
.k-run { background: #dcfce7; color: #166534; }
.k-click, .k-click_at { background: #dbeafe; color: #1e40af; }
.k-wait, .k-hit, .k-text { background: #fef9c3; color: #854d0e; }
.k-sub_task, .k-goto { background: #f3e8ff; color: #6b21a8; }
.k-swipe { background: #cffafe; color: #155e75; }
.k-sleep { background: #fee2e2; color: #991b1b; }
@media (prefers-color-scheme: dark) {
  .k-run { background: #14532d; color: #bbf7d0; }
  .k-click, .k-click_at { background: #1e3a8a; color: #bfdbfe; }
  .k-wait, .k-hit, .k-text { background: #713f12; color: #fef08a; }
  .k-sub_task, .k-goto { background: #4c1d95; color: #e9d5ff; }
  .k-swipe { background: #164e63; color: #a5f3fc; }
  .k-sleep { background: #7f1d1d; color: #fecaca; }
}
.call.shot { margin: 6px 0; padding-top: 7px; padding-bottom: 7px; background: var(--shot-bg);
             border: 1px solid var(--shot-line); border-radius: 10px; }
.call.shot::before { top: 10px; }
.call.shot .note { margin-top: 0; }
.shot-name { color: var(--shot-text); }
.cam { display: inline-block; width: 16px; height: 12px; border: 2px solid var(--shot-text); border-radius: 3px;
       position: relative; vertical-align: -1px; flex: 0 0 auto; }
.cam::after { content: ""; position: absolute; width: 5px; height: 5px; border-radius: 50%; border: 2px solid var(--shot-text);
              left: 50%; top: 50%; transform: translate(-50%, -50%); }
.empty { color: var(--muted); font-size: 13px; margin: 10px 0 0; }
.loose { margin: 8px 0 0; padding-left: 18px; color: var(--muted); font-size: 12.5px; }
.legend { margin-top: 28px; display: flex; flex-wrap: wrap; gap: 8px 18px; font-size: 12px; color: var(--muted); }
.legend > span { display: inline-flex; align-items: center; gap: 6px; }
.legend .cam { border-color: var(--shot-text); }
@media (max-width: 640px) {
  .plats { margin-left: 0; width: 100%; }
  .plan th:first-child { width: 90px; }
}
</style>
</head>
<body>"""

SCRIPT = """<script>
const picks = [...document.querySelectorAll('.pick')];
let platform = '';
let onceOnly = false;
function apply() {
  picks.forEach(button => {
    if (button.dataset.once !== undefined) button.setAttribute('aria-pressed', String(onceOnly));
    else button.setAttribute('aria-pressed', String((button.dataset.platform || '') === platform));
  });
  document.querySelectorAll('.step').forEach(card => {
    const ran = (card.dataset.platforms || '').split(' ').filter(Boolean);
    const offPlatform = platform !== '' && ran.length > 0 && !ran.includes(platform);
    card.classList.toggle('dim', offPlatform || (onceOnly && card.dataset.once !== '1'));
  });
}
picks.forEach(button => button.addEventListener('click', () => {
  if (button.dataset.once !== undefined) onceOnly = !onceOnly;
  else platform = (button.dataset.platform || '') === platform ? '' : (button.dataset.platform || '');
  apply();
}));
apply();
</script>"""
