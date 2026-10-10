import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import test from "node:test";
import {pathToFileURL} from "node:url";

const DATA_FILES = [
    "delivery_jobs.json",
    "delivery_destinations.json",
    "environment_monitoring.json",
    "sell_product.json",
    "teleport_anchors.json",
    "collect_points.json",
];
const CACHE_FILES = [
    ...DATA_FILES,
    "version.txt",
];

const MOCK_SOURCE = `
import fs from "node:fs";
import {basename} from "node:path";
import {syncBuiltinESMExports} from "node:module";
const scenario = JSON.parse(process.env.FETCH_DATA_TEST_SCENARIO);
const originalWrite = fs.writeFileSync;
let failed = false;
fs.writeFileSync = (path, ...args) => {
    if (basename(String(path)) === scenario.writeFailure) {
        if (!failed) {
            failed = true;
            originalWrite(path, "truncated");
            throw new Error("injected write failure");
        }
        if (scenario.restoreFailure) throw new Error("injected restore failure");
    }
    return originalWrite(path, ...args);
};
syncBuiltinESMExports();
globalThis.fetch = async (input) => {
    if (scenario.offline) throw new Error("injected offline");
    const url = new URL(input);
    if (url.pathname.endsWith("/version")) {
        return new Response(JSON.stringify({data: {list: [{version: scenario.version ?? "new"}]}}));
    }
    if (scenario.noDownload) throw new Error("unexpected data download");
    if (url.searchParams.get("ver") !== (scenario.version ?? "new")) {
        throw new Error("missing version query");
    }
    if (scenario.cacheBust && !/^\\d+$/.test(url.searchParams.get("t") ?? "")) {
        throw new Error("missing cache-bust query");
    }
    const file = basename(url.pathname);
    if (file === scenario.httpFailure) return new Response("unavailable", {status: 503});
    if (file === scenario.jsonFailure) return new Response("{broken");
    return new Response('{"generation":"new","id":9007199254740993,"value":1.25e+20}');
};
`;

function fixture(t, {cache = true, missingFile, missingVersion = false, invalidFile} = {}) {
    const root = mkdtempSync(join(tmpdir(), "maaend-fetch-data-test-"));
    t.after(() => rmSync(root, {recursive: true, force: true}));
    const dir = join(root, "tools", "pipeline-generate");
    const dataDir = join(dir, "data");
    mkdirSync(join(dir, "utils"), {recursive: true});
    copyFileSync(new URL("./fetch-data.mjs", import.meta.url), join(dir, "fetch-data.mjs"));
    copyFileSync(new URL("./utils/paths.mjs", import.meta.url), join(dir, "utils", "paths.mjs"));
    if (cache) {
        mkdirSync(dataDir);
        for (const file of DATA_FILES) {
            if (file !== missingFile) {
                writeFileSync(join(dataDir, file), file === invalidFile ? "{broken" : '{ "generation": "old" }\r\n');
            }
        }
        if (!missingVersion) writeFileSync(join(dataDir, "version.txt"), "old\r\n");
    }
    const mock = join(root, "mock-fetch.mjs");
    writeFileSync(mock, MOCK_SOURCE);
    return {
        dataDir,
        snapshot() {
            return Object.fromEntries(
                CACHE_FILES.map((file) => {
                    const path = join(dataDir, file);
                    return [
                        file,
                        existsSync(path) ? readFileSync(path).toString("hex") : null,
                    ];
                }),
            );
        },
        run(scenario = {}, args = []) {
            return spawnSync(
                process.execPath,
                [
                    "--import",
                    pathToFileURL(mock).href,
                    join(dir, "fetch-data.mjs"),
                    ...args,
                ],
                {
                    encoding: "utf8",
                    timeout: 10000,
                    env: {...process.env, GITHUB_ACTIONS: "false", FETCH_DATA_TEST_SCENARIO: JSON.stringify(scenario)},
                },
            );
        },
    };
}

function assertExit(result, expected) {
    assert.ifError(result.error);
    assert.equal(result.status, expected, `${result.stdout}\n${result.stderr}`);
}

test("a later HTTP failure preserves every old cache byte", (t) => {
    const cache = fixture(t);
    const before = cache.snapshot();
    const result = cache.run({httpFailure: DATA_FILES[2]});
    assertExit(result, 0);
    assert.match(result.stderr, /503/);
    assert.deepEqual(cache.snapshot(), before);
});

test("invalid JSON in the last download preserves the complete old cache", (t) => {
    const cache = fixture(t);
    const before = cache.snapshot();
    const result = cache.run({jsonFailure: DATA_FILES.at(-1)}, ["--force"]);
    assertExit(result, 0);
    assert.deepEqual(cache.snapshot(), before);
});

test("a failed first download run leaves no published partial cache", (t) => {
    const cache = fixture(t, {cache: false});
    const before = cache.snapshot();
    assertExit(cache.run({httpFailure: DATA_FILES[2]}), 1);
    assert.deepEqual(cache.snapshot(), before);
});

test("an incomplete old cache cannot mask a download failure", (t) => {
    const cache = fixture(t, {missingFile: DATA_FILES.at(-1)});
    const before = cache.snapshot();
    assertExit(cache.run({httpFailure: DATA_FILES[2]}), 1);
    assert.deepEqual(cache.snapshot(), before);
});

test("readable invalid cached JSON is not a usable fallback", (t) => {
    const cache = fixture(t, {invalidFile: DATA_FILES[0]});
    const before = cache.snapshot();
    assertExit(cache.run({offline: true}), 1);
    assert.deepEqual(cache.snapshot(), before);
});

test("a cache without a version cannot mask a download failure", (t) => {
    const cache = fixture(t, {missingVersion: true});
    const before = cache.snapshot();
    assertExit(cache.run({httpFailure: DATA_FILES[2]}), 1);
    assert.deepEqual(cache.snapshot(), before);
});

test("a truncated data write is rolled back and still exits with failure", (t) => {
    const cache = fixture(t);
    const before = cache.snapshot();
    const result = cache.run({writeFailure: DATA_FILES[2]});
    assertExit(result, 1);
    assert.match(result.stderr, /injected write failure/);
    assert.deepEqual(cache.snapshot(), before);
});

test("a failed version write restores all six files and the old version bytes", (t) => {
    const cache = fixture(t);
    const before = cache.snapshot();
    assertExit(cache.run({writeFailure: "version.txt"}), 1);
    assert.deepEqual(cache.snapshot(), before);
});

test("a failed first cache write removes newly created files", (t) => {
    const cache = fixture(t, {cache: false});
    const before = cache.snapshot();
    assertExit(cache.run({writeFailure: DATA_FILES[2]}), 1);
    assert.deepEqual(cache.snapshot(), before);
});

test("a rollback failure is reported and cannot return success", (t) => {
    const cache = fixture(t);
    const result = cache.run({writeFailure: DATA_FILES[2], restoreFailure: true});
    assertExit(result, 1);
    assert.match(result.stderr, /injected restore failure/);
    assert.equal(readFileSync(join(cache.dataDir, "version.txt"), "utf8"), "old\r\n");
});

test("the current cached version skips data downloads without rewriting files", (t) => {
    const cache = fixture(t);
    const before = cache.snapshot();
    const result = cache.run({version: "old", noDownload: true});
    assertExit(result, 0);
    assert.match(result.stdout, /跳过下载/);
    assert.deepEqual(cache.snapshot(), before);
});

test("a version-check outage can still use the complete old cache", (t) => {
    const cache = fixture(t);
    const before = cache.snapshot();
    assertExit(cache.run({offline: true}), 0);
    assert.deepEqual(cache.snapshot(), before);
});

for (const initialCache of [
    true,
    false,
]) {
    test(`a successful download publishes all files and exact numbers (initial cache: ${initialCache})`, (t) => {
        const cache = fixture(t, {cache: initialCache});
        assertExit(cache.run(), 0);
        for (const file of DATA_FILES) {
            const content = readFileSync(join(cache.dataDir, file), "utf8");
            assert.equal(JSON.parse(content).generation, "new");
            assert.match(content, /9007199254740993/);
            assert.match(content, /1\.25e\+20/);
            assert.ok(content.endsWith("\n"));
        }
        assert.equal(readFileSync(join(cache.dataDir, "version.txt"), "utf8"), "new");
    });
}

test("cache-bust refreshes an unchanged version and keeps version and timestamp queries", (t) => {
    const cache = fixture(t);
    const result = cache.run({version: "old", cacheBust: true}, ["--cache-bust"]);
    assertExit(result, 0);
    assert.match(result.stdout, /ver=old&t=\d+/);
    for (const file of DATA_FILES) {
        assert.equal(JSON.parse(readFileSync(join(cache.dataDir, file), "utf8")).generation, "new");
    }
});
