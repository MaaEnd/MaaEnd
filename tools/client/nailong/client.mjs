import {createHash} from "node:crypto";
import {spawnSync} from "node:child_process";
import {cpSync, existsSync, mkdirSync, readFileSync, readdirSync, symlinkSync, writeFileSync} from "node:fs";
import {dirname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {parseArgs} from "node:util";

const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, "../../..");
const cache = join(root, ".cache/nailong-client");
const stateFile = join(root, ".cache/nailong-client-state.json");
const settings = JSON.parse(readFileSync(join(directory, "client.json"), "utf8"));
const patch = join(directory, "theme.patch");
const assets = join(directory, "assets");
const {values, positionals} = parseArgs({
    allowPositionals: true,
    options: {target: {type: "string"}, output: {type: "string"}},
});
const command = positionals[0] || "prepare";
const targets = {
    "win-x86_64": "x86_64-pc-windows-msvc",
    "win-aarch64": "aarch64-pc-windows-msvc",
    "macos-x86_64": "x86_64-apple-darwin",
    "macos-aarch64": "aarch64-apple-darwin",
    "linux-x86_64": "x86_64-unknown-linux-gnu",
};
const target = values.target ? targets[values.target] : undefined;
if (values.target && !target) throw new Error(`Unsupported client target: ${values.target}`);
if (
    ![
        "prepare",
        "check",
        "test",
        "build",
        "dev",
    ].includes(command)
)
    throw new Error(`Unknown command: ${command}`);

// --- Process execution and source preparation ---

/** Run a fixed tool command and stop immediately when it fails. */
function run(executable, args, cwd = cache, capture = false) {
    const result = spawnSync(executable, args, {
        cwd,
        encoding: "utf8",
        stdio: capture ? "pipe" : "inherit",
        // Only pnpm needs the Windows command shim; all its arguments are fixed or validated above.
        shell: process.platform === "win32" && executable === "pnpm",
    });
    if (result.error) throw result.error;
    if (result.status !== 0)
        throw new Error(`${executable} ${args.join(" ")} failed (${result.status}): ${result.stderr || ""}`);
    return result.stdout?.trim();
}

/** Hash the patch and bundled assets to identify this exact MaaEnd customization. */
function fingerprint() {
    const hash = createHash("sha256").update(JSON.stringify(settings)).update(readFileSync(patch));
    for (const name of readdirSync(assets).sort()) hash.update(name).update(readFileSync(join(assets, name)));
    return hash.digest("hex");
}

/** Apply the MaaEnd-owned customization to the pinned, read-only upstream source. */
function prepare() {
    mkdirSync(join(root, ".cache"), {recursive: true});
    if (existsSync(stateFile)) {
        const state = JSON.parse(readFileSync(stateFile, "utf8"));
        if (state.fingerprint !== fingerprint())
            throw new Error(
                "Client patch changed. Rename .cache/nailong-client and its state file before preparing again.",
            );
        if (
            run(
                "git",
                [
                    "rev-parse",
                    "HEAD",
                ],
                cache,
                true,
            ) !== settings.commit
        )
            throw new Error("Cached client is not at the pinned commit.");
        for (const [
            name,
            digest,
        ] of Object.entries(state.files)) {
            if (
                createHash("sha256")
                    .update(readFileSync(join(cache, name)))
                    .digest("hex") !== digest
            ) {
                throw new Error(
                    `Cached client was edited: ${name}. Save the edit as a MaaEnd patch before rebuilding.`,
                );
            }
        }
        return;
    }
    if (existsSync(cache))
        throw new Error(
            "Client cache exists without a state file. Rename it to preserve its contents before preparing again.",
        );
    run(
        "git",
        [
            "clone",
            "--no-checkout",
            "--filter=blob:none",
            settings.repository,
            cache,
        ],
        root,
    );
    run("git", [
        "checkout",
        "--detach",
        settings.commit,
    ]);
    run("git", [
        "apply",
        "--check",
        patch,
    ]);
    run("git", [
        "apply",
        patch,
    ]);
    cpSync(assets, join(cache, "src/assets/nailong"), {recursive: true});

    // Stamp all package versions together so releases and diagnostics identify this custom client.
    const pkg = JSON.parse(readFileSync(join(cache, "package.json"), "utf8"));
    pkg.version = settings.version;
    writeFileSync(join(cache, "package.json"), `${JSON.stringify(pkg, null, 2)}\n`);
    const tauri = JSON.parse(readFileSync(join(cache, "src-tauri/tauri.conf.json"), "utf8"));
    tauri.version = settings.version;
    writeFileSync(join(cache, "src-tauri/tauri.conf.json"), `${JSON.stringify(tauri, null, 2)}\n`);
    const cargoPath = join(cache, "src-tauri/Cargo.toml");
    // Tauri writes LF here during builds; use the same format before recording the cache digest.
    const cargo = readFileSync(cargoPath, "utf8")
        .replace(/\r\n/g, "\n")
        .replace(/^version = "[^"]+"/m, `version = "${settings.version}"`);
    writeFileSync(cargoPath, cargo);
    const lockPath = join(cache, "src-tauri/Cargo.lock");
    const lock = readFileSync(lockPath, "utf8").replace(
        /(\[\[package\]\]\r?\nname = "mxu"\r?\nversion = ")[^"]+/,
        `$1${settings.version}`,
    );
    writeFileSync(lockPath, lock);

    const names = [...readFileSync(patch, "utf8").matchAll(/^\+\+\+ b\/(.+)$/gm)].map((match) => match[1].trim());
    names.push("package.json", "src-tauri/tauri.conf.json", "src-tauri/Cargo.toml", "src-tauri/Cargo.lock");
    for (const name of readdirSync(assets)) names.push(`src/assets/nailong/${name}`);
    const files = Object.fromEntries(
        names.map((name) => [
            name,
            createHash("sha256")
                .update(readFileSync(join(cache, name)))
                .digest("hex"),
        ]),
    );
    writeFileSync(stateFile, JSON.stringify({fingerprint: fingerprint(), files}, null, 2));
}

// --- Build, validation and local runtime ---

/** Copy MaaEnd interface files and link resource directories into the isolated dev runtime. */
function prepareRuntime() {
    const runtime = join(cache, "src-tauri/target/debug");
    mkdirSync(runtime, {recursive: true});
    for (const name of readdirSync(join(root, "assets"), {withFileTypes: true})) {
        const source = join(root, "assets", name.name);
        const destination = join(runtime, name.name);
        if (name.isDirectory()) {
            if (!existsSync(destination))
                symlinkSync(source, destination, process.platform === "win32" ? "junction" : "dir");
        } else {
            cpSync(source, destination);
        }
    }
    // Use already installed agents and MaaFramework when available.
    for (const name of [
        "agent",
        "maafw",
    ]) {
        const source = join(root, "install", name);
        const destination = join(runtime, name);
        if (existsSync(source) && !existsSync(destination))
            symlinkSync(source, destination, process.platform === "win32" ? "junction" : "dir");
    }
}

/** Build the custom executable and place it at MaaEnd's existing installation boundary. */
function build() {
    run("pnpm", [
        "exec",
        "tauri",
        "build",
        "--no-bundle",
        ...(target
            ? [
                  "--target",
                  target,
              ]
            : []),
    ]);
    const platform = values.target?.split("-")[0] || (process.platform === "win32" ? "win" : process.platform);
    const suffix = platform === "win" ? ".exe" : "";
    const profile = join(
        resolve(cache, process.env.CARGO_TARGET_DIR || "src-tauri/target"),
        ...(target ? [target] : []),
        "release",
    );
    const output = resolve(root, values.output || "install");
    mkdirSync(output, {recursive: true});
    cpSync(join(profile, `mxu${suffix}`), join(output, `MaaEnd${suffix}`));
    if (suffix && existsSync(join(profile, "mxu.pdb"))) cpSync(join(profile, "mxu.pdb"), join(output, "MaaEnd.pdb"));
    writeFileSync(join(output, "nailong-client.json"), `${JSON.stringify(settings, null, 4)}\n`);
    console.log(`MaaEnd custom client installed: ${join(output, `MaaEnd${suffix}`)}`);
}

prepare();
if (command !== "prepare") {
    run("pnpm", [
        "install",
        "--frozen-lockfile",
    ]);
    if (command === "check" || command === "test") {
        run(
            process.execPath,
            [
                "--test",
                join(directory, "client.test.mjs"),
            ],
            root,
        );
        run("pnpm", ["test"]);
        run("pnpm", ["build"]);
        run("cargo", [
            "test",
            "--manifest-path",
            "src-tauri/Cargo.toml",
            "--locked",
        ]);
    } else if (command === "dev") {
        prepareRuntime();
        run("pnpm", [
            "tauri",
            "dev",
        ]);
    } else {
        build();
    }
}
