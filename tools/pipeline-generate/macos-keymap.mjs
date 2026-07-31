// Generate the temporary macOS Pipeline key-code compatibility resource.
// Run with: pnpm generate:MacOS
// Do not edit assets/resource_macos/pipeline/MacOSKeyMap.json manually.
// Remove this generator after MaaFramework MacOS supports use_win32_vk_code.

import fs from "node:fs";
import path from "node:path";
import {parse, printParseErrorCode} from "jsonc-parser";
import prettier from "prettier";
import {repoRoot} from "./utils/paths.mjs";

const pipelineRoot = path.join(repoRoot, "assets/resource/pipeline");
const outputPath = path.join(
    repoRoot,
    "assets/resource_macos/pipeline/MacOSKeyMap.json",
);

const keyboardActionTypes = new Map([
    ["key", "ClickKey"],
    ["clickkey", "ClickKey"],
    ["longpresskey", "LongPressKey"],
    ["keydown", "KeyDown"],
    ["keyup", "KeyUp"],
]);

// Win32 Virtual-Key to CoreGraphics CGKeyCode. The MacOS GlobalEvent input
// consumes these values directly instead of translating Win32 virtual keys.
const win32ToMacOSKeyCode = new Map([
    [0x08, 0x33],
    [0x09, 0x30],
    [0x0d, 0x24],
    [0x10, 0x38],
    [0x11, 0x3b],
    [0x12, 0x3a],
    [0x14, 0x39],
    [0x1b, 0x35],
    [0x20, 0x31],
    [0x21, 0x74],
    [0x22, 0x79],
    [0x23, 0x77],
    [0x24, 0x73],
    [0x25, 0x7b],
    [0x26, 0x7e],
    [0x27, 0x7c],
    [0x28, 0x7d],
    [0x2e, 0x75],
    [0x30, 0x1d],
    [0x31, 0x12],
    [0x32, 0x13],
    [0x33, 0x14],
    [0x34, 0x15],
    [0x35, 0x17],
    [0x36, 0x16],
    [0x37, 0x1a],
    [0x38, 0x1c],
    [0x39, 0x19],
    [0x41, 0x00],
    [0x42, 0x0b],
    [0x43, 0x08],
    [0x44, 0x02],
    [0x45, 0x0e],
    [0x46, 0x03],
    [0x47, 0x05],
    [0x48, 0x04],
    [0x49, 0x22],
    [0x4a, 0x26],
    [0x4b, 0x28],
    [0x4c, 0x25],
    [0x4d, 0x2e],
    [0x4e, 0x2d],
    [0x4f, 0x1f],
    [0x50, 0x23],
    [0x51, 0x0c],
    [0x52, 0x0f],
    [0x53, 0x01],
    [0x54, 0x11],
    [0x55, 0x20],
    [0x56, 0x09],
    [0x57, 0x0d],
    [0x58, 0x07],
    [0x59, 0x10],
    [0x5a, 0x06],
    [0x5b, 0x37],
    [0x70, 0x7a],
    [0x71, 0x78],
    [0x72, 0x63],
    [0x73, 0x76],
    [0x74, 0x60],
    [0x75, 0x61],
    [0x76, 0x62],
    [0x77, 0x64],
    [0x78, 0x65],
    [0x79, 0x6d],
    [0x7a, 0x67],
    [0x7b, 0x6f],
    [0xa0, 0x38],
    [0xa1, 0x3c],
    [0xa2, 0x3b],
    [0xa3, 0x3e],
    [0xa4, 0x3a],
    [0xa5, 0x3d],
]);

function listJsonFiles(root) {
    return fs
        .globSync("**/*.json", { cwd: root })
        .sort()
        .map((relativePath) => path.join(root, relativePath));
}

function parseJsoncFile(filePath) {
    const errors = [];
    const value = parse(fs.readFileSync(filePath, "utf8"), errors, {
        allowTrailingComma: true,
        disallowComments: false,
    });
    if (errors.length > 0) {
        const detail = errors
            .map(
                ({ error, offset }) =>
                    `${printParseErrorCode(error)} at offset ${offset}`,
            )
            .join(", ");
        throw new Error(`Failed to parse ${path.relative(repoRoot, filePath)}: ${detail}`);
    }
    return value;
}

function getExplicitAction(node) {
    if (!node || !("action" in node)) {
        return undefined;
    }
    if (typeof node.action === "string") {
        return { type: node.action, param: node };
    }
    if (node.action && typeof node.action === "object") {
        return { type: node.action.type, param: node.action.param ?? {} };
    }
    return null;
}

function mergeAction(previous, current) {
    if (current === undefined) {
        return previous;
    }
    if (current === null || !current.type) {
        return current;
    }
    if (
        previous?.type &&
        String(previous.type).toLowerCase() === String(current.type).toLowerCase()
    ) {
        return { type: current.type, param: { ...previous.param, ...current.param } };
    }
    return current;
}

function convertKey(key, nodeName, actionType) {
    if (!Number.isInteger(key)) {
        throw new Error(`${nodeName} ${actionType} has a non-integer key: ${String(key)}`);
    }
    const converted = win32ToMacOSKeyCode.get(key);
    if (converted === undefined) {
        throw new Error(
            `${nodeName} ${actionType} uses unknown Win32 VK ${key} (0x${key.toString(16)})`,
        );
    }
    return converted;
}

function createMinimalOverride(nodeName, action) {
    if (!action) {
        return null;
    }
    const actionType = keyboardActionTypes.get(String(action.type).toLowerCase());
    if (!actionType) {
        return null;
    }

    const rawKey = action.param.key;
    const convertedKey = Array.isArray(rawKey)
        ? rawKey.map((key) => convertKey(key, nodeName, actionType))
        : convertKey(rawKey, nodeName, actionType);
    const param = { key: convertedKey };
    if (actionType === "LongPressKey" && action.param.duration !== undefined) {
        param.duration = action.param.duration;
    }
    return {
        action: {
            type: actionType,
            param,
        },
    };
}

const finalActions = new Map();
for (const filePath of listJsonFiles(pipelineRoot)) {
    const pipeline = parseJsoncFile(filePath);
    for (const [nodeName, node] of Object.entries(pipeline ?? {})) {
        const action = mergeAction(finalActions.get(nodeName), getExplicitAction(node));
        if (action !== undefined) {
            finalActions.set(nodeName, action);
        }
    }
}

const output = {};
for (const nodeName of [...finalActions.keys()].sort()) {
    const override = createMinimalOverride(nodeName, finalActions.get(nodeName));
    if (override) {
        output[nodeName] = override;
    }
}

fs.mkdirSync(path.dirname(outputPath), { recursive: true });
const header = `// Generated by: pnpm generate:MacOS\n// DO NOT EDIT MANUALLY.\n// Temporary compatibility layer until MaaFramework MacOS supports use_win32_vk_code.\n`;
const prettierConfig = await prettier.resolveConfig(outputPath);
const formatted = await prettier.format(`${header}${JSON.stringify(output, null, 4)}\n`, {
    ...prettierConfig,
    filepath: outputPath,
});
fs.writeFileSync(outputPath, formatted);
console.log(
    `Generated ${path.relative(repoRoot, outputPath)} with ${Object.keys(output).length} nodes.`,
);
