"""把 capture_flow 画成一张可点的 HTML：按真实运行顺序排，平台切换看得见。"""

from __future__ import annotations

import ast
from dataclasses import dataclass
from html import escape
from pathlib import Path

from .flow import Flow, RouteFunc, Step
from .nodes import Entry, Prepared, Run, chain, load_next_map, plan, prepare
from .runner import Segment, split_segments

KIND_LABEL = {"run": "跑", "click": "点", "wait": "等", "hit": "看", "sub_task": "整串", "goto": "转", "shot": "截图"}

KIND_TEXT = {
    "run": "只跑它自己，next 已清空",
    "click": "用它定位后点一下，不跑它的动作",
    "wait": "等它命中，不点也不动",
    "hit": "看一眼在不在",
}

# 平台配色，按 --controller 给的顺序取。
PALETTE = ["#2f6feb", "#1f9d55", "#b7791f", "#9333ea", "#0e7490", "#c2410c", "#4d7c0f", "#be185d"]


@dataclass
class Card:
    """一步要展示的东西。"""

    index: int
    step: Step
    calls: list[str]
    chains: dict[str, list[str]]
    shots: list[str]
    comments: list[str]


def read_comments(source: Path, name: str) -> list[str]:
    text = source.read_text(encoding="utf-8")
    tree = ast.parse(text, filename=str(source))
    for node in tree.body:
        if isinstance(node, ast.FunctionDef) and node.name == name and node.end_lineno is not None:
            lines = text.splitlines()[node.lineno - 1 : node.end_lineno]
            found: list[str] = []
            for line in lines:
                if "#" in line:
                    comment = line.split("#", 1)[1].strip()
                    if comment:
                        found.append(comment)
            return found
    return []


def route_text(route: str | RouteFunc | None) -> str:
    if route is None:
        return "不写，回大世界（SceneAnyEnterWorld）"
    if isinstance(route, str):
        return route
    return getattr(route, "__name__", "流程函数") + "（自己一步步点过去）"


def call_lines(entry: Entry, chains: dict[str, list[str]]) -> str:
    label = KIND_LABEL.get(entry.kind, entry.kind)
    extra = ""
    if entry.kind == "goto" and entry.route:
        extra = f'<span class="note">不在就跑 {escape(entry.route)}</span>'
    elif entry.name in chains:
        extra = f'<span class="note">连带跑 {escape(" → ".join(chains[entry.name]))}</span>'
    text = KIND_TEXT.get(entry.kind, "")
    note = f'<span class="note">{escape(text)}</span>' if text else ""
    return (
        f'<li><span class="kind {entry.kind}">{escape(label)}</span><code>{escape(entry.name)}</code>{note}{extra}</li>'
    )


def build_cards(flow: Flow, next_map: dict[str, list[str]], prepared: Prepared) -> dict[str, Card]:
    comments = {step.name: read_comments(flow.source, step.name) for step in flow.steps} if flow.source else {}
    cards: dict[str, Card] = {}
    for index, step in enumerate(flow.steps, start=1):
        entries = prepared.entries.get(step.name, [])
        chains: dict[str, list[str]] = {}
        if isinstance(step.route, str):
            chains[step.route] = chain(next_map, step.route)
        for entry in entries:
            if entry.kind == "sub_task":
                chains[entry.name] = chain(next_map, entry.name)
            if entry.route and entry.route not in prepared.entries:
                chains[entry.route] = chain(next_map, entry.route)
        calls = [call_lines(entry, chains) for entry in entries if entry.kind != "shot"]
        if not calls:
            calls = ['<li class="none">这一步不调用 pipeline 节点，只截图</li>']
        cards[step.name] = Card(
            index=index,
            step=step,
            calls=calls,
            chains=chains,
            shots=[entry.name for entry in entries if entry.kind == "shot"],
            comments=comments.get(step.name, []),
        )
    return cards


def card_html(card: Card) -> str:
    step = card.step
    badges: list[str] = []
    if step.once:
        badges.append('<span class="badge once">只能做一次</span>')
    if not step.stop:
        badges.append('<span class="badge join">和下一段连着</span>')
    shots = "".join(f"<li>{escape(name)}</li>" for name in card.shots) or '<li class="none">不截图</li>'
    comments = ""
    if card.comments:
        items = "".join(f"<li>{escape(text)}</li>" for text in card.comments)
        comments = f'<ul class="comments">{items}</ul>'
    enter = escape(step.enter) if step.enter else "<i>没写，不检查界面</i>"
    return (
        f'<li class="step" data-once="{1 if step.once else 0}">'
        f'<div class="no">{card.index}</div><div class="body">'
        f"<h3>{escape(step.name)} {''.join(badges)}</h3>"
        f'<p class="nodes"><b>进这个界面</b> {enter}<br><b>不在就过去</b> {escape(route_text(step.route))}</p>'
        f'<ul class="calls">{"".join(card.calls)}</ul>'
        f'<p class="label">截图</p><ul class="shots">{shots}</ul>{comments}'
        "</div></li>"
    )


def chip(name: str, color: str) -> str:
    return f'<span class="chip" style="--c:{color}">{escape(name)}</span>'


def render(flow: Flow, controllers: tuple[str, ...] = ()) -> str:
    segments = split_segments(flow)
    next_map = load_next_map(Path.cwd())
    prepared = prepare(flow.source, flow.steps) if flow.source is not None else Prepared()
    cards = build_cards(flow, next_map, prepared)
    colors = {name: PALETTE[index % len(PALETTE)] for index, name in enumerate(controllers)}
    allocation = plan(segments, controllers) if controllers else None

    parts: list[str] = [HEAD.replace("{{TITLE}}", escape(flow.name))]
    where = escape(str(flow.source)) if flow.source is not None else "（不是从文件加载的）"
    node_count = len({entry.name for entries in prepared.entries.values() for entry in entries if entry.kind != "shot"})
    parts.append("<header>")
    parts.append(f"<h1>{escape(flow.name)}</h1>")
    parts.append(f'<p class="meta">{where}</p>')
    parts.append(
        f'<p class="meta">{len(flow.steps)} 步 · {len(segments)} 段 · 直接点到 {node_count} 个 pipeline 节点</p>'
    )
    parts.append(run_order(segments, allocation, colors, controllers))
    parts.append(platform_bar(controllers))
    parts.append("</header><main>")

    previous = ""
    for segment in segments:
        runs = allocation.get(segment.index, []) if allocation else []
        if allocation:
            for run in runs:
                if not run.steps:
                    parts.append(
                        f'<p class="off">{chip(run.controller, colors.get(run.controller, "#888"))}'
                        f" 第 {segment.index} 段不用跑（只能做一次的步骤已被别人做完），整段跳过，连都不连</p>"
                    )
                    continue
                if previous and run.controller != previous:
                    parts.append(
                        f'<div class="switch">{chip(previous, colors.get(previous, "#888"))}'
                        f'<span class="arrow">⇄ 顶号换平台</span>'
                        f"{chip(run.controller, colors.get(run.controller, '#888'))}"
                        f'<span class="tip">游戏不关，只换操作的那一端</span></div>'
                    )
                parts.append(pass_block(segment.index, run.controller, run.steps, cards, colors))
                previous = run.controller
        else:
            parts.append(pass_block(segment.index, "", [step.name for _, step in segment.steps], cards, colors))
    parts.append("</main>")
    parts.append(SCRIPT)
    parts.append("</body></html>")
    return "\n".join(parts)


def run_order(
    segments: list[Segment],
    allocation: dict[int, list[Run]] | None,
    colors: dict[str, str],
    controllers: tuple[str, ...],
) -> str:
    if not allocation:
        return (
            '<p class="order-line">没指定平台：每段所有平台都要跑一遍，先后顺序一段正、一段反。'
            "带上 --controller 才看得出谁在哪一段跑、从哪一段起换平台。</p>"
        )
    pieces: list[str] = []
    for segment in segments:
        for run in allocation.get(segment.index, []):
            if not run.steps:
                continue
            pieces.append(
                f'<span class="hop">{chip(run.controller, colors.get(run.controller, "#888"))}'
                f'<span class="seg">段{segment.index}</span></span>'
            )
    chain_html = '<span class="hop-arrow">→</span>'.join(pieces)
    return f'<p class="order-line"><b>运行顺序</b> {chain_html}</p>'


def pass_block(index: int, platform: str, step_names: list[str], cards: dict[str, Card], colors: dict[str, str]) -> str:
    color = colors.get(platform, "#8b949e")
    name = escape(platform) if platform else "每台平台"
    head = (
        f'<div class="pass-head"><span class="dot" style="--c:{color}"></span>'
        f"<b>第 {index} 段</b><span class='plat' style='--c:{color}'>{name}</span>"
        f'<span class="count">{len(step_names)} 步</span></div>'
    )
    body = "".join(card_html(cards[name_]) for name_ in step_names if name_ in cards)
    return f'<section class="pass" style="--c:{color}">{head}<ol class="steps">{body}</ol></section>'


def platform_bar(controllers: tuple[str, ...]) -> str:
    buttons = ['<button class="pick" data-platform="" aria-pressed="true">全部平台</button>']
    for name in controllers:
        buttons.append(f'<button class="pick" data-platform="{escape(name)}">{escape(name)}</button>')
    buttons.append('<button class="pick" data-once="1">只看只能做一次的</button>')
    hint = "点平台名只亮那台平台跑的部分" if controllers else "不带 --controller 就只按一台平台示意"
    return '<div class="bar">' + "".join(buttons) + f'<span class="hint">{hint}</span></div>'


HEAD = """<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>截图流程 · {{TITLE}}</title>
<style>
:root { color-scheme: light dark; }
body { margin: 0; padding: 26px 18px 70px; font: 14px/1.6 -apple-system, "Segoe UI", "Noto Sans CJK SC", sans-serif;
       background: #f6f7f9; color: #1c1f23; }
header, main { max-width: 1000px; margin: 0 auto; }
h1 { margin: 0 0 4px; font-size: 22px; }
.meta { margin: 0; color: #5a6067; font-size: 12px; word-break: break-all; }
.order-line { margin: 14px 0 0; font-size: 13px; color: #33415c; display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.hop { display: inline-flex; align-items: center; gap: 6px; }
.hop-arrow { color: #9aa1a9; }
.seg { font-size: 11px; color: #7c848d; }
.chip { display: inline-block; font-size: 12px; padding: 1px 9px; border-radius: 999px;
        background: color-mix(in srgb, var(--c) 14%, #fff); color: var(--c); border: 1px solid color-mix(in srgb, var(--c) 40%, #fff); }
.bar { margin: 12px 0 22px; display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.pick { font: inherit; font-size: 12px; padding: 4px 12px; border-radius: 999px; cursor: pointer;
        border: 1px solid #cfd6de; background: #fff; color: #33415c; }
.pick[aria-pressed="true"] { background: #33415c; border-color: #33415c; color: #fff; }
.hint { font-size: 12px; color: #7c848d; }
.switch { margin: 4px 0 16px; padding: 12px 16px; border-radius: 10px; background: #fff7e6;
          border: 1px dashed #d9a441; display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.switch .arrow { font-weight: 600; color: #8a5a12; }
.switch .tip { font-size: 12px; color: #8a5a12; }
.off { margin: 6px 0 14px; font-size: 12px; color: #7c848d; }
.pass { background: #fff; border: 1px solid #e2e5e9; border-left: 5px solid var(--c); border-radius: 10px;
        padding: 12px 18px 6px; margin-bottom: 14px; }
.pass-head { display: flex; align-items: center; gap: 10px; font-size: 13px; color: #33415c;
             padding-bottom: 8px; border-bottom: 1px solid #eef1f4; }
.pass-head .dot { width: 9px; height: 9px; border-radius: 50%; background: var(--c); }
.pass-head .plat { color: var(--c); font-weight: 600; }
.pass-head .count { margin-left: auto; font-size: 12px; color: #7c848d; }
.steps { list-style: none; margin: 0; padding: 0; }
.step { display: flex; gap: 12px; padding: 12px 0; border-top: 1px dashed #e2e5e9; }
.step:first-child { border-top: none; }
.step.dim, .pass.dim { opacity: 0.25; }
.no { flex: 0 0 28px; height: 28px; border-radius: 50%; background: #eef1f4; color: #4b5563;
      display: flex; align-items: center; justify-content: center; font-size: 12px; }
.body { flex: 1; min-width: 0; }
.body h3 { margin: 2px 0 4px; font-size: 14px; }
.badge { font-size: 11px; font-weight: normal; padding: 1px 7px; border-radius: 9px; margin-left: 6px;
         background: #eef1f4; color: #4b5563; vertical-align: 1px; }
.badge.once { background: #fde8e4; color: #a03426; }
.badge.join { background: #e8f1fd; color: #1f4e8c; }
.nodes { margin: 0 0 8px; font-size: 12px; color: #4b5563; }
.nodes b { color: #1c1f23; font-weight: 600; }
.calls { list-style: none; margin: 0 0 10px; padding: 0; font-size: 12px; }
.calls li { padding: 2px 0; display: flex; flex-wrap: wrap; gap: 6px; align-items: baseline; }
.calls code { font-size: 12px; background: #f2f4f7; border-radius: 4px; padding: 0 5px; }
.kind { font-size: 11px; padding: 0 6px; border-radius: 4px; background: #eef1f4; color: #33415c; }
.kind.run { background: #e6f4ea; color: #17693a; }
.kind.click { background: #e8f1fd; color: #1f4e8c; }
.kind.wait { background: #fdf1e0; color: #8a5a12; }
.kind.sub_task, .kind.goto { background: #f3e8fd; color: #6b3fa0; }
.note { color: #7c848d; }
.label { margin: 0 0 4px; font-size: 11px; color: #7c848d; }
.shots { margin: 0 0 8px; padding: 0; list-style: none; display: flex; flex-wrap: wrap; gap: 6px; }
.shots li { font-size: 12px; background: #f4f6f8; border: 1px solid #e2e5e9; border-radius: 6px; padding: 1px 8px; }
.calls li.none, .shots li.none { color: #9aa1a9; background: none; border-style: dashed; }
.comments { margin: 4px 0 0; padding-left: 18px; font-size: 12px; color: #6b7280; }
@media (prefers-color-scheme: dark) {
  body { background: #15181b; color: #e6e8ea; }
  .pass { background: #1c2024; border-color: #2a2f35; border-left-color: var(--c); }
  .pass-head { border-bottom-color: #2a2f35; }
  .pick { background: #1c2024; border-color: #2a2f35; color: #c9d1d9; }
  .pick[aria-pressed="true"] { background: #3f5c8a; border-color: #3f5c8a; color: #fff; }
  .switch { background: #2a2314; border-color: #6b5116; }
  .switch .arrow, .switch .tip { color: #e0b862; }
  .meta, .order-line, .who, .nodes, .comments, .note, .hint, .label, .off, .pass-head { color: #9aa1a9; }
  .no, .badge, .kind { background: #262b31; color: #c9d1d9; }
  .calls code, .shots li { background: #22272c; border-color: #2a2f35; }
  .step { border-color: #2a2f35; }
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
  document.querySelectorAll('.pass').forEach(block => {
    const name = block.querySelector('.pass-head .plat');
    const owner = name ? name.textContent.trim() : '';
    block.classList.toggle('dim', platform !== '' && owner !== platform);
  });
  document.querySelectorAll('.step').forEach(card => {
    card.classList.toggle('dim', onceOnly && card.dataset.once !== '1');
  });
}
picks.forEach(button => button.addEventListener('click', () => {
  if (button.dataset.once !== undefined) onceOnly = !onceOnly;
  else platform = (button.dataset.platform || '') === platform ? '' : (button.dataset.platform || '');
  apply();
}));
apply();
</script>"""
