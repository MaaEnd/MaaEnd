import {readFileSync} from "node:fs";
import {resolve} from "node:path";

const [
    ,
    ,
    commitMsgPath,
] = process.argv;

if (!commitMsgPath) {
    console.error("Missing commit message file path.");
    process.exit(1);
}

const repoRoot = resolve(import.meta.dirname, "..");
const cliffTomlPath = resolve(repoRoot, ".github", "cliff.toml");

function extractAllowedTypes(cliffToml) {
    const typeSet = new Set();
    const parserPattern = /message\s*=\s*"(\^[^"]+)"/g;

    for (const match of cliffToml.matchAll(parserPattern)) {
        const source = match[1];

        if (!source.startsWith("^") || source.includes("[skip changelog]")) {
            continue;
        }

        const normalized = source.slice(1).replaceAll("\\\\(", "(").replaceAll("\\\\)", ")");

        if (normalized.startsWith("(")) {
            const groupEnd = normalized.indexOf(")");

            if (groupEnd > 1) {
                for (const type of normalized.slice(1, groupEnd).split("|")) {
                    if (/^[a-z][a-z0-9-]*$/.test(type)) {
                        typeSet.add(type);
                    }
                }
            }

            continue;
        }

        const typeMatch = normalized.match(/^([a-z][a-z0-9-]*)/);

        if (typeMatch) {
            typeSet.add(typeMatch[1]);
        }
    }

    return [...typeSet].sort();
}

const commitMessage = readFileSync(commitMsgPath, "utf8");
const title = commitMessage.split(/\r?\n/, 1)[0]?.trim() ?? "";

if (!title) {
    console.error("Commit title cannot be empty.");
    process.exit(1);
}

const allowedTypes = extractAllowedTypes(readFileSync(cliffTomlPath, "utf8"));

if (allowedTypes.length === 0) {
    console.error(`Failed to parse commit types from ${cliffTomlPath}.`);
    process.exit(1);
}

if (title.length > 72) {
    console.error(`Commit title is too long (${title.length}/72).`);
    process.exit(1);
}

const escapedTypes = allowedTypes.map((type) => type.replace(/[-/\\^$*+?.()|[\]{}]/g, "\\$&"));
const titlePattern = new RegExp(`^(?<type>${escapedTypes.join("|")})(\\([^)\\r\\n]+\\))?(!)?: (?<subject>\\S.*)$`);

if (!titlePattern.test(title)) {
    console.error("Invalid commit title.");
    console.error("Expected: <type>(<scope>)?: <subject> or <type>(<scope>)!: <subject>");
    console.error(`Allowed types: ${allowedTypes.join(", ")}`);
    console.error(`Read from: ${cliffTomlPath}`);
    process.exit(1);
}
