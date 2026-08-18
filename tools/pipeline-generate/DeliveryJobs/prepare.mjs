import {existsSync, rmSync} from "node:fs";
import {isAbsolute, relative, resolve} from "node:path";

import {repoRoot} from "../utils/paths.mjs";

const generatedDirs = [
    resolve(repoRoot, "assets/resource/pipeline/DeliveryJobs/Depot"),
    resolve(repoRoot, "assets/resource/pipeline/DeliveryJobs/Region"),
];

function isInside(parent, child) {
    const path = relative(resolve(parent), resolve(child));
    return path !== "" && !path.startsWith("..") && !isAbsolute(path);
}

for (const generatedDir of generatedDirs) {
    if (!isInside(repoRoot, generatedDir)) {
        throw new Error(`[DeliveryJobs] 生成目录超出仓库范围：${generatedDir}`);
    }
    if (existsSync(generatedDir)) {
        rmSync(generatedDir, {recursive: true});
        console.log(`[DeliveryJobs] 已清理旧生成目录：${generatedDir}`);
    }
}
