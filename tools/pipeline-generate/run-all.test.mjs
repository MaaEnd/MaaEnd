import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {
    copyFileSync,
    existsSync,
    mkdirSync,
    mkdtempSync,
    readFileSync,
    rmSync,
    symlinkSync,
    writeFileSync,
} from "node:fs";
import {createRequire} from "node:module";
import {tmpdir} from "node:os";
import {dirname, isAbsolute, relative, resolve} from "node:path";
import test from "node:test";
import {fileURLToPath, pathToFileURL} from "node:url";

const sourceRunner = fileURLToPath(new URL("./run-all.mjs", import.meta.url));
const require = createRequire(import.meta.url);
const realPackageRoot = dirname(require.resolve("@joebao/maa-pipeline-generate/package.json"));
const oldOutput = Buffer.from('{"Stale":{"value":"keep on failure"}}\r\n');

function fixture(t, {task = false, fakeBin} = {}) {
    const root = mkdtempSync(resolve(tmpdir(), "maaend-generator-recovery-"));
    t.after(() => {
        const rel = relative(resolve(tmpdir()), root);
        assert.ok(rel && !rel.startsWith("..") && !isAbsolute(rel), "cleanup must stay inside the temporary directory");
        rmSync(root, {recursive: true, force: true});
    });
    const generatorDir = resolve(root, "tools/pipeline-generate");
    const configDir = resolve(generatorDir, "Fixture");
    const outputDir = resolve(root, "assets/output");
    const packageDir = resolve(root, "node_modules/@joebao/maa-pipeline-generate");
    mkdirSync(configDir, {recursive: true});
    mkdirSync(outputDir, {recursive: true});
    mkdirSync(dirname(packageDir), {recursive: true});
    const runner = resolve(generatorDir, "run-all.mjs");
    copyFileSync(sourceRunner, runner);
    if (fakeBin) {
        mkdirSync(packageDir);
        writeFileSync(resolve(packageDir, "package.json"), JSON.stringify({bin: "bin.mjs"}));
        writeFileSync(resolve(packageDir, "bin.mjs"), fakeBin);
    } else {
        symlinkSync(realPackageRoot, packageDir, process.platform === "win32" ? "junction" : "dir");
    }
    const config = {
        task,
        merged: !task,
        template: "template.json",
        data: "data.json",
        outputDir: "../../../assets/output",
        outputFile: "generated.json",
        format: false,
    };
    const configPath = resolve(configDir, "config.json");
    writeFileSync(configPath, JSON.stringify(config));
    writeFileSync(resolve(configDir, "data.json"), JSON.stringify([{Id: "Current"}]));
    writeFileSync(
        resolve(configDir, "template.json"),
        task
            ? '{"task":[{"name":"${Id}","entry":"${Id}"}],"option":{}}'
            : '{"${Id}":{"recognition":"DirectHit","action":"DoNothing"}}',
    );
    const output = resolve(outputDir, "generated.json");
    const other = resolve(outputDir, "manually-maintained.json");
    writeFileSync(other, "untouched\r\n");
    return {
        root,
        config,
        configPath,
        output,
        run(nodeArgs = []) {
            const result = spawnSync(
                process.execPath,
                [
                    ...nodeArgs,
                    runner,
                    "Fixture",
                ],
                {encoding: "utf8"},
            );
            assert.equal(readFileSync(other, "utf8"), "untouched\r\n", "other output files must remain unchanged");
            return result;
        },
    };
}

for (const task of [
    false,
    true,
]) {
    const mode = task ? "task" : "merged";
    test(`${mode}: real generator restores original bytes when data is missing`, (t) => {
        const f = fixture(t, {task});
        writeFileSync(f.output, oldOutput);
        writeFileSync(f.configPath, JSON.stringify({...f.config, data: "missing.json"}));
        const result = f.run();
        assert.equal(result.status, 1, result.stdout + result.stderr);
        assert.deepEqual(readFileSync(f.output), oldOutput);
        assert.match(result.stdout, /restored previous output state/);
    });

    test(`${mode}: real generator successful output removes stale entries`, (t) => {
        const f = fixture(t, {task});
        const old = task ? {task: [{name: "Stale", entry: "Stale"}], option: {StaleOption: {}}} : {Stale: {}};
        writeFileSync(f.output, JSON.stringify(old));
        const result = f.run();
        assert.equal(result.status, 0, result.stdout + result.stderr);
        const generated = JSON.parse(readFileSync(f.output, "utf8"));
        if (task) {
            assert.deepEqual(
                generated.task.map(({name}) => name),
                ["Current"],
            );
            assert.equal(generated.option.StaleOption, undefined);
        } else {
            assert.deepEqual(Object.keys(generated), ["Current"]);
        }
    });
}

const partialFailureBin = `
import {writeFileSync} from "node:fs";
writeFileSync("../../../assets/output/generated.json", "partial output");
process.exit(7);
`;

test("partial output is replaced with original bytes after generator failure", (t) => {
    const f = fixture(t, {fakeBin: partialFailureBin});
    writeFileSync(f.output, oldOutput);
    const result = f.run();
    assert.equal(result.status, 7, result.stdout + result.stderr);
    assert.deepEqual(readFileSync(f.output), oldOutput);
});

test("partial output is removed when there was no original file", (t) => {
    const f = fixture(t, {fakeBin: partialFailureBin});
    const result = f.run();
    assert.equal(result.status, 7, result.stdout + result.stderr);
    assert.equal(existsSync(f.output), false);
});

test("invalid config is rejected before deleting existing output", (t) => {
    const f = fixture(t, {fakeBin: partialFailureBin});
    writeFileSync(f.output, oldOutput);
    writeFileSync(f.configPath, "{ invalid json");
    const result = f.run();
    assert.equal(result.status, 1, result.stdout + result.stderr);
    assert.deepEqual(readFileSync(f.output), oldOutput);
    assert.match(result.stderr, /failed to inspect/);
    assert.doesNotMatch(result.stdout, /removed stale/);
});

test("output directories are rejected before deleting their contents", (t) => {
    const f = fixture(t, {fakeBin: partialFailureBin});
    mkdirSync(f.output);
    const marker = resolve(f.output, "keep.txt");
    writeFileSync(marker, "keep");
    const result = f.run();
    assert.equal(result.status, 1, result.stdout + result.stderr);
    assert.equal(readFileSync(marker, "utf8"), "keep");
    assert.match(result.stderr, /outputFile is not a regular file/);
    assert.doesNotMatch(result.stdout, /removed stale/);
});

test("output symlinks are rejected before touching their target", (t) => {
    const f = fixture(t, {fakeBin: partialFailureBin});
    const target = resolve(f.root, "linked-output");
    mkdirSync(target);
    const marker = resolve(target, "keep.txt");
    writeFileSync(marker, "keep");
    symlinkSync(target, f.output, process.platform === "win32" ? "junction" : "dir");
    const result = f.run();
    assert.equal(result.status, 1, result.stdout + result.stderr);
    assert.equal(readFileSync(marker, "utf8"), "keep");
    assert.match(result.stderr, /outputFile is not a regular file/);
    assert.doesNotMatch(result.stdout, /removed stale/);
});

test("spawn errors restore original output", (t) => {
    const f = fixture(t, {fakeBin: partialFailureBin});
    writeFileSync(f.output, oldOutput);
    const hook = resolve(f.root, "spawn-error.mjs");
    writeFileSync(
        hook,
        `import childProcess from "node:child_process";
import {syncBuiltinESMExports} from "node:module";
childProcess.spawnSync = () => ({status: null, error: new Error("fixture spawn failure")});
syncBuiltinESMExports();`,
    );
    const result = f.run([
        "--import",
        pathToFileURL(hook).href,
    ]);
    assert.equal(result.status, 1, result.stdout + result.stderr);
    assert.deepEqual(readFileSync(f.output), oldOutput);
    assert.match(result.stderr, /fixture spawn failure/);
});

test("terminated generator restores original output", (t) => {
    const f = fixture(t, {fakeBin: 'process.kill(process.pid, "SIGTERM");'});
    writeFileSync(f.output, oldOutput);
    const result = f.run();
    assert.notEqual(result.status, 0, result.stdout + result.stderr);
    assert.deepEqual(readFileSync(f.output), oldOutput);
});

test("restoration failure is reported and never returns success", (t) => {
    const f = fixture(t, {
        fakeBin:
            'import {mkdirSync} from "node:fs"; mkdirSync("../../../assets/output/generated.json"); process.exit(7);',
    });
    writeFileSync(f.output, oldOutput);
    const result = f.run();
    assert.equal(result.status, 1, result.stdout + result.stderr);
    assert.match(result.stderr, /failed to restore/);
    assert.doesNotMatch(result.stdout, /restored previous output state/);
});
