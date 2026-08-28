import {readFileSync} from "node:fs";

import {parse, printParseErrorCode} from "jsonc-parser";

export function parseJsonc(text, source = "input") {
    const errors = [];
    const value = parse(text, errors, {
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
