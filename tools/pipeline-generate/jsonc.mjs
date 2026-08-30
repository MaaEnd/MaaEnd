import {readFileSync} from "node:fs";

import {parse, printParseErrorCode} from "jsonc-parser";

export function parseJsonc(text, source = "input") {
    // readFileSync 会保留 UTF-8 BOM，而 jsonc-parser 不接受前导 U+FEFF；仅移除文件开头的一个 BOM。
    const content = text.startsWith("\uFEFF") ? text.slice(1) : text;
    const errors = [];
    const value = parse(content, errors, {
        allowTrailingComma: true,
        disallowComments: false,
    });
    if (errors.length > 0) {
        const detail = errors.map(({error, offset}) => `${printParseErrorCode(error)} at offset ${offset}`).join(", ");
        throw new Error(`[pipeline-generate] 无法解析 JSONC ${source}: ${detail}`);
    }
    return value;
}

export function readJsonc(path) {
    return parseJsonc(readFileSync(path, "utf8"), String(path));
}
