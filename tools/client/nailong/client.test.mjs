import {test} from "node:test";
import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {readFileSync, writeFileSync} from "node:fs";
import {dirname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";

const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, "../../..");
const script = join(directory, "client.mjs");

/** Exercise the real preparation CLI, including its exit status and diagnostics. */
function invoke(args) {
    return spawnSync(
        process.execPath,
        [
            script,
            ...args,
        ],
        {cwd: root, encoding: "utf8"},
    );
}

test("preparation is repeatable and uses the pinned official source", () => {
    for (let attempt = 0; attempt < 2; attempt += 1) {
        const result = invoke(["prepare"]);
        assert.equal(result.status, 0, result.stderr);
    }
    const settings = JSON.parse(readFileSync(join(directory, "client.json"), "utf8"));
    const head = spawnSync(
        "git",
        [
            "rev-parse",
            "HEAD",
        ],
        {cwd: join(root, ".cache/nailong-client"), encoding: "utf8"},
    );
    assert.equal(head.stdout.trim(), settings.commit);
    const installedAsset = readFileSync(join(root, ".cache/nailong-client/src/assets/nailong/working.gif"));
    assert.deepEqual(installedAsset, readFileSync(join(directory, "assets/working.gif")));
});

test("preparation preserves and rejects local edits instead of overwriting them", () => {
    const file = join(root, ".cache/nailong-client/src/components/NailongCompanion.tsx");
    const original = readFileSync(file);
    try {
        writeFileSync(
            file,
            Buffer.concat([
                original,
                Buffer.from("\n// cache preservation probe\n"),
            ]),
        );
        const result = invoke(["prepare"]);
        assert.notEqual(result.status, 0);
        assert.match(result.stderr, /Cached client was edited/);
        assert.match(readFileSync(file, "utf8"), /cache preservation probe/);
    } finally {
        writeFileSync(file, original);
    }
});

test("unsupported targets fail before source preparation or shell execution", () => {
    const result = invoke([
        "build",
        "--target",
        "unsupported-target",
    ]);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Unsupported client target/);
});
