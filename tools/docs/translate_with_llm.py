from __future__ import annotations

import argparse
import dataclasses
import datetime
import html
import json
import os
import posixpath
import re
import shutil
import unicodedata
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable, Iterator
from pathlib import Path, PurePosixPath
from typing import Any


DEFAULT_API_STYLE = "openai"
DEFAULT_MAX_TOKENS = 8192
DEFAULT_ANTHROPIC_BASE_URL = "https://api.anthropic.com"
DEFAULT_OPENAI_BASE_URL = "https://api.openai.com"
DEFAULT_GEMINI_BASE_URL = "https://generativelanguage.googleapis.com"
DEFAULT_COPILOT_BASE_URL = "https://api.githubcopilot.com"
DEFAULT_COPILOT_MODEL = "gpt-4o"
ANTHROPIC_API_VERSION = "2023-06-01"
SOURCE_DOC_ROOT = "docs/zh_cn"
TARGET_DOC_ROOT = "docs/en_us"
DEFAULT_STATE_PATH = Path("docs/en_us/.docs-sync-state.json")
LINK_PLACEHOLDER_PREFIX = "__MAAEND_LINK_"
DOC_LINK_SUFFIXES = {
    "",
    ".md",
    ".markdown",
    ".mdx",
    ".txt",
}
INLINE_LINK_RE = re.compile(r"(!?\[[^\]\n]+\]\()([^)]+)(\))")
REFERENCE_LINK_RE = re.compile(r"(?m)^(\s{0,3}\[[^\]\n]+\]:\s*)(\S.*)$")
HTML_LINK_ATTR_RE = re.compile(r"\b(href|src)=(['\"])(.*?)(\2)", re.IGNORECASE)
HEADING_MARKER_PREFIX = "__MAAEND_HEADING_"
HEADING_MARKER_RE = re.compile(r"[ \t]*__MAAEND_HEADING_(\d{4})__")
BLOCKQUOTE_PREFIX_RE = re.compile(r"^(?: {0,3}>[ \t]?)+")
FENCE_RE = re.compile(r"^[ \t]*(`{3,}|~{3,})(.*)$")
ATX_HEADING_RE = re.compile(r"^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$")
CODE_SPAN_RE = re.compile(r"(`+)(.+?)(?<!`)\1(?!`)")
INLINE_LINK_TEXT_RE = re.compile(r"(!?)\[([^\]]*)\](?:\([^)]*\)|\[[^\]]*\])")
HTML_COMMENT_RE = re.compile(r"<!--.*?-->", re.DOTALL)
HTML_TAG_RE = re.compile(r"(?<!\\)</?[A-Za-z][^>]*>")
ESCAPED_PUNCTUATION_RE = re.compile(r"\\([!-/:-@\[-`{-~])")
ESCAPED_UNDERSCORE = "\ue000"
ANCHOR_DOC_SUFFIXES = {".md", ".markdown", ".mdx"}
DEFAULT_MODELS = {
    "anthropic": "claude-3-5-haiku-latest",
    "openai": "gpt-4.1-mini",
    "gemini": "gemini-2.0-flash",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Translate docs manifest tasks with a single LLM config secret.",
    )
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--state-path", default=DEFAULT_STATE_PATH, type=Path)
    parser.add_argument(
        "--translator",
        choices=["config", "copilot"],
        default="config",
        help="Translation backend. Copilot is opt-in for manual runs.",
    )
    return parser.parse_args()


def read_manifest(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def write_sync_state(path: Path, manifest: dict[str, Any]) -> None:
    state = {
        "source_lang": manifest.get("source_lang", "zh_cn"),
        "target_lang": manifest.get("target_lang", "en_us"),
        "source_count": manifest.get("source_count", 0),
        "sources": manifest.get("sources", {}),
        "updated_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    }
    ensure_parent(path)
    with path.open("w", encoding="utf-8", newline="\n") as handle:
        handle.write(json.dumps(state, ensure_ascii=False, indent=4))
        handle.write("\n")


def repo_root() -> Path:
    return Path.cwd().resolve()


def safe_repo_path(path_value: Any, *, expected_prefix: str, must_exist: bool = False) -> Path:
    if not isinstance(path_value, str) or not path_value:
        raise RuntimeError(f"Invalid path value: {path_value!r}")

    normalized = normalize_repo_path(path_value)
    pure_path = PurePosixPath(normalized)
    if pure_path.is_absolute() or any(part == ".." for part in pure_path.parts):
        raise RuntimeError(f"Refusing unsafe path: {path_value}")
    if not normalized.startswith(f"{expected_prefix}/"):
        raise RuntimeError(f"Path must be under {expected_prefix}/: {path_value}")

    candidate = Path(normalized)
    resolved = candidate.resolve(strict=must_exist)
    root = repo_root()
    expected_root = (root / expected_prefix).resolve(strict=True)
    if not resolved.is_relative_to(expected_root):
        raise RuntimeError(f"Path escapes {expected_prefix}/: {path_value}")
    return candidate


def source_doc_path(path_value: Any) -> Path:
    return safe_repo_path(path_value, expected_prefix=SOURCE_DOC_ROOT, must_exist=True)


def target_doc_path(path_value: Any) -> Path:
    return safe_repo_path(path_value, expected_prefix=TARGET_DOC_ROOT, must_exist=False)


def unwrap_code_fence(text: str) -> str:
    stripped = text.strip()
    if not stripped.startswith("```"):
        return stripped

    lines = stripped.splitlines()
    if len(lines) >= 3 and lines[0].startswith("```") and lines[-1].strip() == "```":
        return "\n".join(lines[1:-1]).strip("\n")
    return stripped


def build_prompt(source_text: str) -> str:
    return (
        "Translate the following Markdown content from Simplified Chinese to English.\n"
        "Return only the translated Markdown content.\n"
        "Preserve the Markdown structure exactly where practical, including headings, lists, tables, and blank lines.\n"
        "Do not translate code blocks, inline code, commands, file paths, JSON keys, identifiers, version strings, or placeholders.\n"
        f"Keep every {HEADING_MARKER_PREFIX}NNNN__ marker unchanged at the end of its heading line.\n"
        "Do not add new facts, summaries, or commentary.\n"
        "Do not wrap the output in code fences.\n\n"
        f"{source_text}"
    )


def normalize_repo_path(path: str) -> str:
    normalized = path.replace("\\", "/")
    while normalized.startswith("./"):
        normalized = normalized[2:]
    return normalized


def split_link_destination(destination: str) -> tuple[str, str, str, str]:
    stripped = destination.strip()
    if not stripped:
        return "", "", "", ""

    prefix = destination[: len(destination) - len(destination.lstrip())]
    suffix = destination[len(destination.rstrip()):]
    core = stripped
    quote = ""
    if len(core) >= 2 and core[0] == "<" and core[-1] == ">":
        quote = "angle"
        core = core[1:-1]
    return prefix, core, quote, suffix


def join_link_destination(prefix: str, core: str, quote: str, suffix: str) -> str:
    if quote == "angle":
        core = f"<{core}>"
    return f"{prefix}{core}{suffix}"


def rewrite_doc_link(destination: str, source_path: Path, target_path: Path) -> str:
    prefix, core, quote, suffix = split_link_destination(destination)
    if not core:
        return destination

    parsed = urllib.parse.urlsplit(core)
    if parsed.scheme or parsed.netloc:
        return destination

    path = urllib.parse.unquote(parsed.path)
    if not path:
        return destination

    keep_dot_prefix = path.startswith("./")
    normalized_source = normalize_repo_path(source_path.as_posix())
    normalized_target = normalize_repo_path(target_path.as_posix())
    source_dir = posixpath.dirname(normalized_source)
    target_dir = posixpath.dirname(normalized_target)
    normalized_path = normalize_repo_path(path)

    if normalized_path.startswith(f"{SOURCE_DOC_ROOT}/"):
        target_link_path = f"{TARGET_DOC_ROOT}/{normalized_path[len(SOURCE_DOC_ROOT) + 1:]}"
        output_path = posixpath.relpath(target_link_path, target_dir)
    else:
        source_link_path = posixpath.normpath(posixpath.join(source_dir, normalized_path))
        if not source_link_path.startswith(f"{SOURCE_DOC_ROOT}/"):
            return destination

        suffix_name = PurePosixPath(source_link_path).suffix.lower()
        if suffix_name not in DOC_LINK_SUFFIXES:
            return destination

        relative_to_source_root = source_link_path[len(SOURCE_DOC_ROOT) + 1:]
        target_link_path = f"{TARGET_DOC_ROOT}/{relative_to_source_root}"
        output_path = posixpath.relpath(target_link_path, target_dir)

    if parsed.path.endswith("/") and not output_path.endswith("/"):
        output_path += "/"
    if keep_dot_prefix and not output_path.startswith(("./", "../")):
        output_path = f"./{output_path}"
    encoded_output_path = urllib.parse.quote(output_path, safe="/-._~")
    rewritten_core = urllib.parse.urlunsplit(("", "", encoded_output_path, parsed.query, parsed.fragment))
    return join_link_destination(prefix, rewritten_core, quote, suffix)


def protect_links(source_text: str, source_path: Path, target_path: Path) -> tuple[str, dict[str, str]]:
    placeholders: dict[str, str] = {}

    def next_placeholder(destination: str) -> str:
        placeholder = f"{LINK_PLACEHOLDER_PREFIX}{len(placeholders):04d}__"
        placeholders[placeholder] = rewrite_doc_link(destination, source_path, target_path)
        return placeholder

    def replace_inline(match: re.Match[str]) -> str:
        return f"{match.group(1)}{next_placeholder(match.group(2))}{match.group(3)}"

    def replace_reference(match: re.Match[str]) -> str:
        destination, separator, title = match.group(2).partition(" ")
        rewritten = next_placeholder(destination)
        if separator:
            rewritten = f"{rewritten}{separator}{title}"
        return f"{match.group(1)}{rewritten}"

    def replace_html_attr(match: re.Match[str]) -> str:
        return f"{match.group(1)}={match.group(2)}{next_placeholder(match.group(3))}{match.group(4)}"

    protected = INLINE_LINK_RE.sub(replace_inline, source_text)
    protected = REFERENCE_LINK_RE.sub(replace_reference, protected)
    protected = HTML_LINK_ATTR_RE.sub(replace_html_attr, protected)
    return protected, placeholders


def restore_links(translated_text: str, placeholders: dict[str, str]) -> str:
    restored = translated_text
    for placeholder, destination in placeholders.items():
        restored = restored.replace(placeholder, destination)
    return restored


@dataclasses.dataclass
class HeadingAnchors:
    # zh_cn 标题锚点 -> en_us 标题锚点；targets 是 en_us 文档里实际存在的全部锚点
    mapping: dict[str, str]
    targets: set[str]


@dataclasses.dataclass
class TranslatedDoc:
    path: Path
    text: str
    anchors: HeadingAnchors


def iter_lines_outside_fences(lines: list[str]) -> Iterator[int]:
    fence = ""
    for index, line in enumerate(lines):
        match = FENCE_RE.match(BLOCKQUOTE_PREFIX_RE.sub("", line))
        if not fence:
            if match and not (match.group(1)[0] == "`" and "`" in match.group(2)):
                fence = match.group(1)
                continue
            yield index
        elif (
            match
            and match.group(1)[0] == fence[0]
            and len(match.group(1)) >= len(fence)
            and not match.group(2).strip()
        ):
            fence = ""


def markdown_headings(text: str) -> list[tuple[int, int, str]]:
    lines = text.splitlines()
    headings: list[tuple[int, int, str]] = []
    for index in iter_lines_outside_fences(lines):
        # 引用块（包括提示块）里的标题也有锚点
        match = ATX_HEADING_RE.match(BLOCKQUOTE_PREFIX_RE.sub("", lines[index]))
        if match:
            headings.append((index, len(match.group(1)), match.group(2) or ""))
    return headings


def char_kind(char: str) -> str:
    if not char or char.isspace():
        return "space"
    if unicodedata.category(char)[0] in "PS":
        return "punct"
    return "word"


def strip_underscore_emphasis(text: str) -> str:
    # 按 CommonMark 的 flanking 规则去掉作为强调符号的 `_`，词内的 `_`（如 max_hit）保留
    removed: set[int] = set()
    openers: list[tuple[int, int]] = []
    for match in re.finditer(r"_+", text):
        start, end = match.span()
        before = char_kind(text[start - 1] if start else "")
        after = char_kind(text[end] if end < len(text) else "")
        left_flanking = after != "space" and (after != "punct" or before != "word")
        right_flanking = before != "space" and (before != "punct" or after != "word")
        if openers and right_flanking and (not left_flanking or after == "punct"):
            open_start, open_end = openers.pop()
            count = min(open_end - open_start, end - start)
            removed.update(range(open_end - count, open_end))
            removed.update(range(start, start + count))
        elif left_flanking and (not right_flanking or before == "punct"):
            openers.append((start, end))
    return "".join(char for index, char in enumerate(text) if index not in removed)


def inline_plain_text(text: str) -> str:
    text = HTML_COMMENT_RE.sub("", text)
    text = HTML_TAG_RE.sub("", text)
    text = INLINE_LINK_TEXT_RE.sub(lambda match: "" if match.group(1) else match.group(2), text)
    text = ESCAPED_PUNCTUATION_RE.sub(
        lambda match: ESCAPED_UNDERSCORE if match.group(1) == "_" else match.group(1),
        text,
    )
    text = strip_underscore_emphasis(text)
    return html.unescape(text).replace(ESCAPED_UNDERSCORE, "_")


def heading_plain_text(content: str) -> str:
    # GitHub 用渲染后的标题文字生成锚点：行内代码保留内容，图片不计入
    parts: list[str] = []
    position = 0
    for match in CODE_SPAN_RE.finditer(content):
        parts.append(inline_plain_text(content[position : match.start()]))
        code = match.group(2)
        if code.startswith(" ") and code.endswith(" ") and code.strip():
            code = code[1:-1]
        parts.append(code)
        position = match.end()
    parts.append(inline_plain_text(content[position:]))
    return "".join(parts)


def github_slug(text: str) -> str:
    # 与 GitHub 一致：转小写，只保留字母、数字、连接符、空格和 -，空格换成 -（连续空格不合并）
    kept = []
    for char in text.lower():
        category = unicodedata.category(char)
        if char in "- " or category[0] in "LM" or category in ("Nd", "Nl", "Pc"):
            kept.append(char)
    return "".join(kept).replace(" ", "-")


def heading_anchors(text: str) -> list[str]:
    # 与 GitHub 一致：重名时加 -1、-2…，候选已被占用（包括标题本身就叫 xxx-1）就继续递增；
    # 文字为空的标题（如只有图片）也参与计数：第一个没有锚点，之后的依次是 -1、-2…
    occurrences: dict[str, int] = {}
    anchors: list[str] = []
    for _, _, content in markdown_headings(text):
        base = github_slug(heading_plain_text(content))
        anchor = base
        while anchor in occurrences:
            occurrences[base] += 1
            anchor = f"{base}-{occurrences[base]}"
        occurrences[anchor] = 0
        anchors.append(anchor)
    return anchors


def heading_levels(text: str) -> list[int]:
    return [level for _, level, _ in markdown_headings(text)]


def mark_headings(text: str) -> str:
    # 给每个标题行末尾加编号标记，翻译后据此把中文标题和英文标题一一对应起来
    lines = text.splitlines(keepends=True)
    for ordinal, (index, _, _) in enumerate(markdown_headings(text)):
        body = lines[index].rstrip("\r\n")
        lines[index] = f"{body.rstrip()} {HEADING_MARKER_PREFIX}{ordinal:04d}__{lines[index][len(body):]}"
    return "".join(lines)


def unmark_headings(source_text: str, translated_text: str) -> tuple[str, HeadingAnchors]:
    lines = translated_text.splitlines(keepends=True)
    heading_ordinals = {
        index: ordinal for ordinal, (index, _, _) in enumerate(markdown_headings(translated_text))
    }
    pairs: dict[int, int] = {}
    kept_lines: list[str] = []
    for index, line in enumerate(lines):
        markers = HEADING_MARKER_RE.findall(line)
        if index in heading_ordinals:
            for marker in markers:
                pairs.setdefault(int(marker), heading_ordinals[index])
        stripped = HEADING_MARKER_RE.sub("", line)
        if markers and not stripped.strip():
            continue
        kept_lines.append(stripped)
    text = "".join(kept_lines)

    # 模型丢了标记时，标题层级完全一致就按位置补齐
    if heading_levels(source_text) == heading_levels(text):
        for ordinal in range(len(heading_ordinals)):
            pairs.setdefault(ordinal, ordinal)

    source_anchors = heading_anchors(source_text)
    target_anchors = heading_anchors(text)
    mapping = {
        source_anchors[source]: target_anchors[target]
        for source, target in pairs.items()
        if source < len(source_anchors)
        and target < len(target_anchors)
        and source_anchors[source]
        and target_anchors[target]
    }
    return text, HeadingAnchors(mapping, {anchor for anchor in target_anchors if anchor})


def existing_doc_anchors(target: str) -> HeadingAnchors:
    # 本次没有翻译的文档：中文源文件自上次同步后没变，按标题位置对应
    target_path = target_doc_path(target)
    if not target_path.is_file():
        return HeadingAnchors({}, set())
    target_text = target_path.read_text(encoding="utf-8")
    target_anchors = heading_anchors(target_text)
    source_path = Path(f"{SOURCE_DOC_ROOT}/{target[len(TARGET_DOC_ROOT) + 1:]}")
    mapping: dict[str, str] = {}
    if source_path.is_file():
        source_text = source_path.read_text(encoding="utf-8")
        if heading_levels(source_text) == heading_levels(target_text):
            mapping = {
                source: target
                for source, target in zip(heading_anchors(source_text), target_anchors)
                if source and target
            }
    return HeadingAnchors(mapping, {anchor for anchor in target_anchors if anchor})


def localize_destination(
    destination: str,
    current_target: str,
    resolve: Callable[[str], HeadingAnchors],
    unresolved: list[tuple[str, str, str]],
) -> str:
    prefix, core, quote, suffix = split_link_destination(destination)
    if not core:
        return destination

    parsed = urllib.parse.urlsplit(core)
    if parsed.scheme or parsed.netloc or not parsed.fragment:
        return destination

    path = urllib.parse.unquote(parsed.path)
    if not path:
        linked = current_target
    elif path.startswith("/"):
        linked = posixpath.normpath(path.lstrip("/"))
    else:
        linked = posixpath.normpath(posixpath.join(posixpath.dirname(current_target), path))
    if (
        not linked.startswith(f"{TARGET_DOC_ROOT}/")
        or PurePosixPath(linked).suffix.lower() not in ANCHOR_DOC_SUFFIXES
    ):
        return destination

    fragment = urllib.parse.unquote(parsed.fragment)
    anchors = resolve(linked)
    localized = anchors.mapping.get(fragment, anchors.mapping.get(fragment.lower()))
    if localized is None:
        if fragment not in anchors.targets and fragment.lower() not in anchors.targets:
            unresolved.append((current_target, linked, fragment))
        return destination

    rewritten_core = urllib.parse.urlunsplit(("", "", parsed.path, parsed.query, localized))
    return join_link_destination(prefix, rewritten_core, quote, suffix)


def localize_anchor_links(
    text: str,
    current_target: str,
    resolve: Callable[[str], HeadingAnchors],
    unresolved: list[tuple[str, str, str]],
) -> str:
    def localize(destination: str) -> str:
        return localize_destination(destination, current_target, resolve, unresolved)

    def replace_inline(match: re.Match[str]) -> str:
        return f"{match.group(1)}{localize(match.group(2))}{match.group(3)}"

    def replace_reference(match: re.Match[str]) -> str:
        destination, separator, title = match.group(2).partition(" ")
        return f"{match.group(1)}{localize(destination)}{separator}{title}"

    def replace_html_attr(match: re.Match[str]) -> str:
        return f"{match.group(1)}={match.group(2)}{localize(match.group(3))}{match.group(4)}"

    def localize_segment(segment: str) -> str:
        segment = INLINE_LINK_RE.sub(replace_inline, segment)
        segment = REFERENCE_LINK_RE.sub(replace_reference, segment)
        return HTML_LINK_ATTR_RE.sub(replace_html_attr, segment)

    # 代码块和行内代码里的链接不动
    lines = text.splitlines(keepends=True)
    for index in iter_lines_outside_fences([line.rstrip("\r\n") for line in lines]):
        line = lines[index]
        parts: list[str] = []
        position = 0
        for match in CODE_SPAN_RE.finditer(line):
            parts.append(localize_segment(line[position : match.start()]))
            parts.append(match.group(0))
            position = match.end()
        parts.append(localize_segment(line[position:]))
        lines[index] = "".join(parts)
    return "".join(lines)


def write_translations(translations: dict[str, TranslatedDoc]) -> None:
    anchors_by_target = {target: doc.anchors for target, doc in translations.items()}

    def resolve(target: str) -> HeadingAnchors:
        if target not in anchors_by_target:
            anchors_by_target[target] = existing_doc_anchors(target)
        return anchors_by_target[target]

    unresolved: list[tuple[str, str, str]] = []
    for target, doc in translations.items():
        text = localize_anchor_links(doc.text, target, resolve, unresolved)
        ensure_parent(doc.path)
        doc.path.write_text(text, encoding="utf-8")

    for current_target, linked, fragment in unresolved:
        print(f"Warning: could not map anchor #{fragment} in {current_target} (links to {linked}); fix it by hand.")


def parse_key_value_config(raw_config: str) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for line in raw_config.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        key, separator, value = stripped.partition("=")
        if not separator:
            raise RuntimeError("DOCS_TRANSLATION_CONFIG must be JSON or key=value lines.")
        result[key.strip()] = value.strip().strip('"').strip("'")
    return result


def read_translation_config() -> dict[str, Any]:
    raw_config = os.environ.get("DOCS_TRANSLATION_CONFIG", "").strip()
    if not raw_config:
        raise RuntimeError("Missing DOCS_TRANSLATION_CONFIG secret.")

    if raw_config.startswith("{"):
        config = json.loads(raw_config)
        if not isinstance(config, dict):
            raise RuntimeError("DOCS_TRANSLATION_CONFIG JSON must be an object.")
        return config

    return parse_key_value_config(raw_config)


def config_value(config: dict[str, Any], *names: str, default: Any = "") -> Any:
    normalized = {str(key).lower(): value for key, value in config.items()}
    for name in names:
        value = normalized.get(name.lower())
        if value not in (None, ""):
            return value
    return default


def infer_api_style(config: dict[str, Any]) -> str:
    explicit = str(config_value(config, "api_style", "apiStyle", "style")).strip().lower()
    if explicit:
        if explicit not in DEFAULT_MODELS:
            raise RuntimeError(
                "Unsupported api_style in DOCS_TRANSLATION_CONFIG. "
                "Expected one of: anthropic, openai, gemini."
            )
        return explicit

    base_url = str(config_value(config, "base_url", "baseUrl", "url")).lower()
    model = str(config_value(config, "model")).lower()
    if "generativelanguage.googleapis.com" in base_url:
        return "gemini"
    if "anthropic.com" in base_url:
        return "anthropic"
    if base_url:
        return "openai"
    if model.startswith("gemini"):
        return "gemini"
    if model.startswith("claude"):
        return "anthropic"
    return DEFAULT_API_STYLE


def resolved_translation_config() -> dict[str, Any]:
    config = read_translation_config()
    api_style = infer_api_style(config)
    api_key = str(config_value(config, "api_key", "apiKey", "key", "token")).strip()
    if not api_key:
        raise RuntimeError("DOCS_TRANSLATION_CONFIG must include api_key.")

    model = str(config_value(config, "model", default=DEFAULT_MODELS[api_style])).strip()
    base_url = str(config_value(config, "base_url", "baseUrl", "url")).strip()
    max_tokens = int(config_value(config, "max_tokens", "maxTokens", default=DEFAULT_MAX_TOKENS))
    return {
        "api_style": api_style,
        "api_key": api_key,
        "base_url": base_url,
        "model": model,
        "max_tokens": max_tokens,
    }


def resolved_copilot_config() -> dict[str, Any]:
    token = os.environ.get("COPILOT_GITHUB_TOKEN", "").strip()
    if not token:
        raise RuntimeError("Missing COPILOT_GITHUB_TOKEN secret for Copilot translation.")

    return {
        "api_style": "copilot",
        "api_key": token,
        "base_url": DEFAULT_COPILOT_BASE_URL,
        "model": os.environ.get("COPILOT_TRANSLATION_MODEL", DEFAULT_COPILOT_MODEL).strip(),
        "max_tokens": int(os.environ.get("COPILOT_TRANSLATION_MAX_TOKENS", DEFAULT_MAX_TOKENS)),
    }


def normalize_base_url(api_style: str, base_url: str) -> str:
    candidate = (base_url or "").strip().rstrip("/")
    if not candidate:
        defaults = {
            "anthropic": DEFAULT_ANTHROPIC_BASE_URL,
            "openai": DEFAULT_OPENAI_BASE_URL,
            "gemini": DEFAULT_GEMINI_BASE_URL,
        }
        return defaults[api_style]

    suffixes = {
        "anthropic": ["/v1/messages", "/v1"],
        "openai": ["/v1/chat/completions", "/chat/completions", "/v1"],
        "gemini": ["/v1beta", "/v1"],
    }
    for suffix in suffixes[api_style]:
        if candidate.endswith(suffix):
            return candidate[: -len(suffix)]
    return candidate


def http_post_json(url: str, body: dict[str, Any], headers: dict[str, str]) -> dict[str, Any]:
    request = urllib.request.Request(
        url,
        data=json.dumps(body).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=180) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        error_body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"API error {exc.code}: {error_body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"Failed to reach LLM API: {exc}") from exc


def anthropic_translate(
    *,
    api_key: str,
    base_url: str,
    model: str,
    max_tokens: int,
    prompt: str,
) -> str:
    payload = {
        "model": model,
        "max_tokens": max_tokens,
        "temperature": 0,
        "messages": [
            {
                "role": "user",
                "content": prompt,
            }
        ],
    }
    response = http_post_json(
        f"{normalize_base_url('anthropic', base_url)}/v1/messages",
        payload,
        {
            "content-type": "application/json",
            "x-api-key": api_key,
            "anthropic-version": ANTHROPIC_API_VERSION,
        },
    )
    texts = [
        block.get("text", "")
        for block in response.get("content", [])
        if block.get("type") == "text"
    ]
    return unwrap_code_fence("\n".join(texts))


def flatten_openai_content(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        texts: list[str] = []
        for item in content:
            if isinstance(item, dict) and item.get("type") == "text":
                texts.append(item.get("text", ""))
        return "\n".join(texts)
    return ""


def openai_translate(
    *,
    api_key: str,
    base_url: str,
    model: str,
    max_tokens: int,
    prompt: str,
) -> str:
    payload = {
        "model": model,
        "messages": [
            {
                "role": "system",
                "content": "You are a precise Markdown documentation translator.",
            },
            {
                "role": "user",
                "content": prompt,
            },
        ],
        "temperature": 0,
        "max_tokens": max_tokens,
    }
    response = http_post_json(
        f"{normalize_base_url('openai', base_url)}/v1/chat/completions",
        payload,
        {
            "content-type": "application/json",
            "authorization": f"Bearer {api_key}",
        },
    )
    choices = response.get("choices", [])
    if not choices:
        raise RuntimeError("OpenAI-style API returned no choices.")
    message = choices[0].get("message", {})
    return unwrap_code_fence(flatten_openai_content(message.get("content", "")))


def copilot_translate(
    *,
    api_key: str,
    base_url: str,
    model: str,
    max_tokens: int,
    prompt: str,
) -> str:
    payload = {
        "model": model,
        "messages": [
            {
                "role": "system",
                "content": "You are a precise Markdown documentation translator.",
            },
            {
                "role": "user",
                "content": prompt,
            },
        ],
        "temperature": 0,
        "max_tokens": max_tokens,
    }
    response = http_post_json(
        f"{base_url.rstrip('/')}/chat/completions",
        payload,
        {
            "content-type": "application/json",
            "authorization": f"Bearer {api_key}",
            "copilot-integration-id": "vscode-chat",
            "editor-version": "vscode/1.100.0",
            "editor-plugin-version": "copilot-chat/0.27.0",
            "user-agent": "GitHubCopilotChat/0.27.0",
        },
    )
    choices = response.get("choices", [])
    if not choices:
        raise RuntimeError("Copilot API returned no choices.")
    message = choices[0].get("message", {})
    return unwrap_code_fence(flatten_openai_content(message.get("content", "")))


def gemini_translate(
    *,
    api_key: str,
    base_url: str,
    model: str,
    max_tokens: int,
    prompt: str,
) -> str:
    payload = {
        "system_instruction": {
            "parts": [
                {
                    "text": "You are a precise Markdown documentation translator.",
                }
            ]
        },
        "contents": [
            {
                "role": "user",
                "parts": [
                    {
                        "text": prompt,
                    }
                ],
            }
        ],
        "generationConfig": {
            "temperature": 0,
            "maxOutputTokens": max_tokens,
        },
    }
    root = normalize_base_url("gemini", base_url)
    query = urllib.parse.urlencode({"key": api_key})
    response = http_post_json(
        f"{root}/v1beta/models/{model}:generateContent?{query}",
        payload,
        {
            "content-type": "application/json",
        },
    )
    candidates = response.get("candidates", [])
    if not candidates:
        raise RuntimeError("Gemini API returned no candidates.")
    parts = candidates[0].get("content", {}).get("parts", [])
    texts = [part.get("text", "") for part in parts if isinstance(part, dict)]
    return unwrap_code_fence("\n".join(texts))


def translate_text(
    *,
    api_style: str,
    api_key: str,
    base_url: str,
    model: str,
    max_tokens: int,
    source_text: str,
) -> str:
    prompt = build_prompt(source_text)
    if api_style == "anthropic":
        translated = anthropic_translate(
            api_key=api_key,
            base_url=base_url,
            model=model,
            max_tokens=max_tokens,
            prompt=prompt,
        )
    elif api_style == "openai":
        translated = openai_translate(
            api_key=api_key,
            base_url=base_url,
            model=model,
            max_tokens=max_tokens,
            prompt=prompt,
        )
    elif api_style == "gemini":
        translated = gemini_translate(
            api_key=api_key,
            base_url=base_url,
            model=model,
            max_tokens=max_tokens,
            prompt=prompt,
        )
    elif api_style == "copilot":
        translated = copilot_translate(
            api_key=api_key,
            base_url=base_url,
            model=model,
            max_tokens=max_tokens,
            prompt=prompt,
        )
    else:
        raise RuntimeError(f"Unsupported api_style: {api_style}")

    if not translated:
        raise RuntimeError("Empty translation returned.")
    if source_text.endswith("\n") and not translated.endswith("\n"):
        translated += "\n"
    return translated


def ensure_parent(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)


def translate_task(
    task: dict[str, Any],
    *,
    api_style: str,
    api_key: str,
    base_url: str,
    model: str,
    max_tokens: int,
) -> TranslatedDoc:
    source_path = source_doc_path(task["source_path"])
    target_path = target_doc_path(task["target_path"])
    source_text = source_path.read_text(encoding="utf-8")
    protected_text, placeholders = protect_links(source_text, source_path, target_path)
    translated = translate_text(
        api_style=api_style,
        api_key=api_key,
        base_url=base_url,
        model=model,
        max_tokens=max_tokens,
        source_text=mark_headings(protected_text),
    )
    translated = restore_links(translated, placeholders)
    translated, anchors = unmark_headings(source_text, translated)
    print(f"Translated {task['source_path']} -> {task['target_path']}")
    return TranslatedDoc(target_path, translated, anchors)


def apply_delete(task: dict[str, Any]) -> None:
    target_path = target_doc_path(task["target_path"])
    if target_path.exists():
        target_path.unlink()
        print(f"Deleted {task['target_path']}")
    else:
        print(f"Skip delete; target missing: {task['target_path']}")


def apply_rename(task: dict[str, Any]) -> None:
    old_path = target_doc_path(task["target_path_before"])
    new_path = target_doc_path(task["target_path_after"])
    ensure_parent(new_path)
    if old_path.exists():
        shutil.move(str(old_path), str(new_path))
        print(f"Renamed {task['target_path_before']} -> {task['target_path_after']}")
    else:
        print(f"Skip rename move; old target missing: {task['target_path_before']}")


def main() -> int:
    args = parse_args()
    if args.translator == "copilot":
        llm_config = resolved_copilot_config()
    else:
        llm_config = resolved_translation_config()
    api_style = llm_config["api_style"]
    api_key = llm_config["api_key"]
    model = llm_config["model"]
    base_url = llm_config["base_url"]
    max_tokens = llm_config["max_tokens"]

    manifest = read_manifest(args.manifest)
    tasks = manifest.get("tasks", [])

    if not tasks:
        print("No translation tasks in manifest.")
        return 0

    translations: dict[str, TranslatedDoc] = {}
    try:
        for task in tasks:
            mode = task["mode"]
            if mode == "delete_target":
                apply_delete(task)
                continue
            if mode == "rename_target":
                apply_rename(task)
                translations[normalize_repo_path(task["target_path_after"])] = translate_task(
                    {
                        "source_path": task["source_path_after"],
                        "target_path": task["target_path_after"],
                    },
                    api_style=api_style,
                    api_key=api_key,
                    base_url=base_url,
                    model=model,
                    max_tokens=max_tokens,
                )
                continue
            if mode == "translate_file":
                translations[normalize_repo_path(task["target_path"])] = translate_task(
                    task,
                    api_style=api_style,
                    api_key=api_key,
                    base_url=base_url,
                    model=model,
                    max_tokens=max_tokens,
                )
                continue
            raise RuntimeError(f"Unsupported task mode: {mode}")
    finally:
        # 后面的任务失败时，已经翻译好的文件照样写入，异常继续向上抛；同步状态只在全部成功后更新
        write_translations(translations)

    write_sync_state(args.state_path, manifest)
    print(f"Updated docs sync state: {args.state_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
