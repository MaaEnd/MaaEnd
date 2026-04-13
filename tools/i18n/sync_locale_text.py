#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
import sys
import textwrap
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter, defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Sequence, Tuple


REPO_ROOT = Path(__file__).resolve().parents[2]
TASKS_DIR = REPO_ROOT / "assets" / "tasks"
INTERFACE_FILE = REPO_ROOT / "assets" / "interface.json"
INTERFACE_LOCALE_DIR = REPO_ROOT / "assets" / "locales" / "interface"
GO_SERVICE_LOCALE_DIR = REPO_ROOT / "assets" / "locales" / "go-service"
DEFAULT_LLM_CONFIG_PATH = Path(__file__).resolve().with_name("llm.json")
TRANSLATE_TRIGGER = "TR"

DEFAULT_API_BASE_URL = "https://api.deepseek.com/v1"
DEFAULT_MODEL = "deepseek-chat"

SUPPORTED_LOCALE_ORDER = ["zh_cn", "en_us", "zh_tw", "ja_jp", "ko_kr"]
PRINTF_TOKEN_RE = re.compile(
    r"%(?:\[[^\]]+\])?[+#0\- ]*(?:\d+|\*)?(?:\.(?:\d+|\*))?[a-zA-Z%]"
)
BRACE_TOKEN_RE = re.compile(r"\{[a-zA-Z0-9_]+\}")
HTML_TAG_RE = re.compile(r"</?[^>]+?>")
FENCE_RE = re.compile(r"^```(?:json)?\s*|\s*```$", re.IGNORECASE)


@dataclass(frozen=True)
class EmptyTranslation:
    group: str
    locale: str
    key: str
    source_locale: Optional[str]
    source_text: Optional[str]


@dataclass(frozen=True)
class LLMConfig:
    api_base_url: str
    api_key: str
    model: str


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Sync locale keys from assets and/or translate empty locale entries.",
    )
    parser.add_argument(
        "trigger",
        nargs="?",
        default="",
        help="Optional trigger command. Input TR to start translation directly.",
    )
    parser.add_argument(
        "--phase",
        choices=["generate", "translate", "all"],
        default="generate",
        help="Select workflow phase: generate keys, translate empty texts, or run all.",
    )
    parser.add_argument(
        "--write",
        action="store_true",
        help="Write changes back to locale files.",
    )
    parser.add_argument(
        "--report",
        type=Path,
        default=None,
        help="Write a JSON report to this path.",
    )
    parser.add_argument(
        "--batch-size",
        type=int,
        default=20,
        help="Maximum translation items per API request.",
    )
    parser.add_argument(
        "--llm-config",
        type=Path,
        default=DEFAULT_LLM_CONFIG_PATH,
        help="Path to local llm.json config file.",
    )
    parser.add_argument(
        "--fail-on-translate-error",
        action="store_true",
        help="Exit with non-zero status if any translation request fails.",
    )
    return parser.parse_args()


def load_llm_config(path: Path) -> LLMConfig:
    if not path.is_file():
        raise FileNotFoundError(f"LLM config file not found: {path}")

    payload = load_json(path)
    if not isinstance(payload, dict):
        raise ValueError("LLM config must be a JSON object")

    api_base_url = str(payload.get("api_base_url", DEFAULT_API_BASE_URL)).strip()
    api_key = str(payload.get("api_key", "")).strip()
    model = str(payload.get("model", DEFAULT_MODEL)).strip()

    if not api_base_url:
        raise ValueError("llm.json field api_base_url cannot be empty")
    if not api_key:
        raise ValueError("llm.json field api_key cannot be empty")
    if not model:
        raise ValueError("llm.json field model cannot be empty")

    return LLMConfig(api_base_url=api_base_url, api_key=api_key, model=model)


def strip_jsonc(text: str) -> str:
    result: List[str] = []
    index = 0
    in_string = False
    escaped = False
    while index < len(text):
        char = text[index]
        next_char = text[index + 1] if index + 1 < len(text) else ""

        if in_string:
            result.append(char)
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                in_string = False
            index += 1
            continue

        if char == '"':
            in_string = True
            result.append(char)
            index += 1
            continue

        if char == "/" and next_char == "/":
            index += 2
            while index < len(text) and text[index] not in "\r\n":
                index += 1
            continue

        if char == "/" and next_char == "*":
            index += 2
            while index + 1 < len(text) and not (
                text[index] == "*" and text[index + 1] == "/"
            ):
                index += 1
            index += 2
            continue

        result.append(char)
        index += 1

    stripped = "".join(result)
    trailing_comma_re = re.compile(r",(?=\s*[}\]])")
    return trailing_comma_re.sub("", stripped)


def load_json(path: Path) -> Dict[str, Any]:
    with path.open("r", encoding="utf-8") as file:
        return json.loads(strip_jsonc(file.read()))


def dump_json(path: Path, data: Dict[str, str]) -> None:
    with path.open("w", encoding="utf-8", newline="\n") as file:
        json.dump(data, file, ensure_ascii=False, indent=4)
        file.write("\n")


def iter_strings(value: Any) -> Iterable[str]:
    if isinstance(value, str):
        yield value
        return
    if isinstance(value, list):
        for item in value:
            yield from iter_strings(item)
        return
    if isinstance(value, dict):
        for item in value.values():
            yield from iter_strings(item)


def collect_referenced_keys() -> List[str]:
    referenced_keys: List[str] = []
    seen: set[str] = set()

    files = sorted(TASKS_DIR.rglob("*.json")) + [INTERFACE_FILE]
    for path in files:
        data = load_json(path)
        for text in iter_strings(data):
            if not text.startswith("$"):
                continue
            if text in seen:
                continue
            seen.add(text)
            referenced_keys.append(text[1:])
    return referenced_keys


def load_flat_locale_dir(locale_dir: Path) -> Dict[str, Dict[str, str]]:
    locale_map: Dict[str, Dict[str, str]] = {}
    for path in sorted(locale_dir.glob("*.json")):
        data = load_json(path)
        if not isinstance(data, dict):
            raise ValueError(f"Locale file is not an object: {path}")
        normalized: Dict[str, str] = {}
        for key, value in data.items():
            if isinstance(value, str):
                normalized[key] = value
            else:
                raise ValueError(f"Locale value must be a string: {path} -> {key}")
        locale_map[path.stem] = normalized
    return locale_map


def sync_interface_keys(
    locale_data: Dict[str, Dict[str, str]],
    referenced_keys: Sequence[str],
) -> Dict[str, List[str]]:
    additions: Dict[str, List[str]] = {}
    for locale, data in locale_data.items():
        missing_keys = [key for key in referenced_keys if key not in data]
        if not missing_keys:
            additions[locale] = []
            continue
        for key in missing_keys:
            data[key] = ""
        additions[locale] = missing_keys
    return additions


def locale_sort_key(locale: str) -> Tuple[int, str]:
    try:
        return (SUPPORTED_LOCALE_ORDER.index(locale), locale)
    except ValueError:
        return (len(SUPPORTED_LOCALE_ORDER), locale)


def choose_source_text(
    locale_entries: Dict[str, Dict[str, str]], key: str
) -> Tuple[Optional[str], Optional[str]]:
    for locale in sorted(locale_entries, key=locale_sort_key):
        text = locale_entries[locale].get(key)
        if text is None or text == "":
            continue
        return locale, text
    return None, None


def collect_empty_translations(
    group: str, locale_entries: Dict[str, Dict[str, str]]
) -> List[EmptyTranslation]:
    keys: set[str] = set()
    for entries in locale_entries.values():
        keys.update(entries.keys())

    empty_items: List[EmptyTranslation] = []
    for key in sorted(keys):
        source_locale, source_text = choose_source_text(locale_entries, key)
        for locale, entries in sorted(
            locale_entries.items(), key=lambda item: locale_sort_key(item[0])
        ):
            value = entries.get(key)
            if value not in ("", None):
                continue
            empty_items.append(
                EmptyTranslation(
                    group=group,
                    locale=locale,
                    key=key,
                    source_locale=source_locale,
                    source_text=source_text,
                )
            )
    return empty_items


def extract_tokens(text: str) -> Counter[str]:
    tokens = Counter()
    for token in PRINTF_TOKEN_RE.findall(text):
        if token != "%%":
            tokens[token] += 1
    for token in BRACE_TOKEN_RE.findall(text):
        tokens[token] += 1
    for token in HTML_TAG_RE.findall(text):
        tokens[token] += 1
    return tokens


def strip_code_fence(content: str) -> str:
    return FENCE_RE.sub("", content).strip()


def parse_response_json(content: str) -> Dict[str, str]:
    payload = json.loads(strip_code_fence(content))
    if not isinstance(payload, dict):
        raise ValueError("Translation response is not a JSON object")
    result: Dict[str, str] = {}
    for key, value in payload.items():
        if not isinstance(value, str):
            raise ValueError(f"Translation for {key} is not a string")
        result[str(key)] = value
    return result


def build_chat_request(
    model: str, items: Sequence[EmptyTranslation], target_locale: str
) -> Dict[str, Any]:
    requested_items = [
        {
            "key": item.key,
            "source_locale": item.source_locale,
            "source_text": item.source_text,
        }
        for item in items
    ]

    system_prompt = textwrap.dedent(
        f"""
        You are a professional software localization engine.
        Translate each source_text into locale {target_locale}.
        Rules:
        1. Return JSON only. The output must be a single JSON object mapping key -> translated string.
        2. Preserve placeholders exactly, including printf tokens like %s and %d, named placeholders like {{name}}, HTML tags, markdown structure, emojis, punctuation, and line breaks.
        3. Never return empty strings.
        4. Do not add explanations, comments, or extra keys.
        5. Keep the original tone and UI meaning.
        """
    ).strip()
    user_prompt = json.dumps(requested_items, ensure_ascii=False)

    return {
        "model": model,
        "temperature": 0.2,
        "response_format": {"type": "json_object"},
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
    }


def request_translation(
    *,
    api_base_url: str,
    api_key: str,
    model: str,
    items: Sequence[EmptyTranslation],
    target_locale: str,
) -> Dict[str, str]:
    if not api_key:
        raise ValueError("API key is empty")

    base_url = api_base_url.rstrip("/")
    url = f"{base_url}/chat/completions"
    payload = build_chat_request(model, items, target_locale)
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            raw = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Translation API HTTP {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"Translation API request failed: {exc}") from exc

    body = json.loads(raw)
    choices = body.get("choices")
    if not isinstance(choices, list) or not choices:
        raise ValueError("Translation API response does not contain choices")
    message = choices[0].get("message", {})
    content = message.get("content")
    if not isinstance(content, str):
        raise ValueError("Translation API response does not contain string content")
    return parse_response_json(content)


def validate_translation(source_text: str, translated_text: str) -> Optional[str]:
    if translated_text == "":
        return "translated text is empty"
    if extract_tokens(source_text) != extract_tokens(translated_text):
        return "placeholder or tag mismatch"
    return None


def chunked(
    items: Sequence[EmptyTranslation], size: int
) -> Iterable[Sequence[EmptyTranslation]]:
    for index in range(0, len(items), size):
        yield items[index : index + size]


def translate_empty_entries(
    empty_items: Sequence[EmptyTranslation],
    *,
    api_base_url: str,
    api_key: str,
    model: str,
    batch_size: int,
) -> Tuple[Dict[Tuple[str, str, str], str], List[Dict[str, str]]]:
    translated: Dict[Tuple[str, str, str], str] = {}
    errors: List[Dict[str, str]] = []
    grouped: Dict[Tuple[str, str], List[EmptyTranslation]] = defaultdict(list)
    for item in empty_items:
        if not item.source_text:
            continue
        grouped[(item.group, item.locale)].append(item)

    for (group, locale), group_items in sorted(grouped.items()):
        for batch in chunked(group_items, batch_size):
            try:
                response = request_translation(
                    api_base_url=api_base_url,
                    api_key=api_key,
                    model=model,
                    items=batch,
                    target_locale=locale,
                )
            except Exception as exc:
                for item in batch:
                    errors.append(
                        {
                            "group": group,
                            "locale": locale,
                            "key": item.key,
                            "error": str(exc),
                        }
                    )
                continue

            for item in batch:
                translated_text = response.get(item.key)
                if translated_text is None:
                    errors.append(
                        {
                            "group": group,
                            "locale": locale,
                            "key": item.key,
                            "error": "missing key in translation response",
                        }
                    )
                    continue

                validation_error = validate_translation(
                    item.source_text or "", translated_text
                )
                if validation_error is not None:
                    errors.append(
                        {
                            "group": group,
                            "locale": locale,
                            "key": item.key,
                            "error": validation_error,
                        }
                    )
                    continue

                translated[(group, locale, item.key)] = translated_text
    return translated, errors


def write_locale_dirs(
    interface_locales: Dict[str, Dict[str, str]],
    go_service_locales: Dict[str, Dict[str, str]],
) -> None:
    for locale, data in sorted(
        interface_locales.items(), key=lambda item: locale_sort_key(item[0])
    ):
        dump_json(INTERFACE_LOCALE_DIR / f"{locale}.json", data)
    for locale, data in sorted(
        go_service_locales.items(), key=lambda item: locale_sort_key(item[0])
    ):
        dump_json(GO_SERVICE_LOCALE_DIR / f"{locale}.json", data)


def build_report(
    *,
    referenced_keys: Sequence[str],
    interface_additions: Dict[str, List[str]],
    empty_items: Sequence[EmptyTranslation],
    translated_entries: Dict[Tuple[str, str, str], str],
    unresolved_items: Sequence[EmptyTranslation],
    translation_errors: Sequence[Dict[str, str]],
    token_present: bool,
) -> Dict[str, Any]:
    return {
        "token_present": token_present,
        "referenced_interface_keys": len(referenced_keys),
        "interface_missing_keys": {
            locale: entries
            for locale, entries in interface_additions.items()
            if entries
        },
        "empty_translations": [
            {
                "group": item.group,
                "locale": item.locale,
                "key": item.key,
                "source_locale": item.source_locale,
                "source_text": item.source_text,
            }
            for item in empty_items
        ],
        "translated_entries": [
            {
                "group": group,
                "locale": locale,
                "key": key,
                "text": text,
            }
            for (group, locale, key), text in sorted(translated_entries.items())
        ],
        "unresolved_without_source": [
            {
                "group": item.group,
                "locale": item.locale,
                "key": item.key,
            }
            for item in unresolved_items
        ],
        "translation_errors": list(translation_errors),
        "summary": {
            "interface_missing_key_count": sum(
                len(entries) for entries in interface_additions.values()
            ),
            "empty_translation_count": len(empty_items),
            "translated_count": len(translated_entries),
            "unresolved_without_source_count": len(unresolved_items),
            "translation_error_count": len(translation_errors),
        },
    }


def print_report(report: Dict[str, Any]) -> None:
    summary = report["summary"]
    print(
        "[i18n] interface missing keys:",
        summary["interface_missing_key_count"],
    )
    print(
        "[i18n] empty translations:",
        summary["empty_translation_count"],
    )
    print(
        "[i18n] translated entries:",
        summary["translated_count"],
    )
    print(
        "[i18n] unresolved without source:",
        summary["unresolved_without_source_count"],
    )
    print(
        "[i18n] translation errors:",
        summary["translation_error_count"],
    )

    missing = report["interface_missing_keys"]
    if missing:
        print("[i18n] interface missing key details:")
        for locale, keys in missing.items():
            print(f"  - {locale}: {len(keys)}")
            for key in keys:
                print(f"    * {key}")

    if report["unresolved_without_source"]:
        print("[i18n] unresolved empty translations without source text:")
        for item in report["unresolved_without_source"]:
            print(f"  - {item['group']} / {item['locale']} / {item['key']}")

    if report["translation_errors"]:
        print("[i18n] translation errors:")
        for item in report["translation_errors"]:
            print(
                f"  - {item['group']} / {item['locale']} / {item['key']}: {item['error']}",
            )


def main() -> int:
    args = parse_args()
    trigger = str(args.trigger).strip().upper()
    phase = args.phase
    if trigger:
        if trigger != TRANSLATE_TRIGGER:
            print(f"[i18n] unknown trigger: {args.trigger}")
            print(f"[i18n] supported trigger: {TRANSLATE_TRIGGER}")
            return 2
        phase = "translate"

    interface_locales = load_flat_locale_dir(INTERFACE_LOCALE_DIR)
    go_service_locales = load_flat_locale_dir(GO_SERVICE_LOCALE_DIR)

    referenced_keys: List[str] = []
    interface_additions: Dict[str, List[str]] = {
        locale: [] for locale in interface_locales.keys()
    }
    if phase in {"generate", "all"}:
        referenced_keys = collect_referenced_keys()
        interface_additions = sync_interface_keys(interface_locales, referenced_keys)

    empty_items = collect_empty_translations("interface", interface_locales)
    empty_items.extend(collect_empty_translations("go-service", go_service_locales))

    unresolved_items = [item for item in empty_items if not item.source_text]
    translatable_items = [item for item in empty_items if item.source_text]

    translated_entries: Dict[Tuple[str, str, str], str] = {}
    translation_errors: List[Dict[str, str]] = []
    token_present = False
    if phase in {"translate", "all"} and translatable_items:
        llm_config = load_llm_config(args.llm_config)
        token_present = bool(llm_config.api_key)
        translated_entries, translation_errors = translate_empty_entries(
            translatable_items,
            api_base_url=llm_config.api_base_url,
            api_key=llm_config.api_key,
            model=llm_config.model,
            batch_size=max(1, args.batch_size),
        )

    for (group, locale, key), text in translated_entries.items():
        if group == "interface":
            interface_locales[locale][key] = text
        elif group == "go-service":
            go_service_locales[locale][key] = text

    if args.write:
        write_locale_dirs(interface_locales, go_service_locales)

    report = build_report(
        referenced_keys=referenced_keys,
        interface_additions=interface_additions,
        empty_items=empty_items,
        translated_entries=translated_entries,
        unresolved_items=unresolved_items,
        translation_errors=translation_errors,
        token_present=token_present,
    )
    print_report(report)
    if phase == "generate":
        print(
            f"[i18n] generation finished. After you manually complete one locale, run with '{TRANSLATE_TRIGGER}' to translate others.",
        )

    if args.report is not None:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        with args.report.open("w", encoding="utf-8", newline="\n") as file:
            json.dump(report, file, ensure_ascii=False, indent=4)
            file.write("\n")

    if args.fail_on_translate_error and translation_errors:
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
