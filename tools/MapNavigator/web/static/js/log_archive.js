/**
 * Minimal browser-local ZIP reader for MaaEnd log bundles. It reads the central
 * directory first, inflates only cpp-algo logs and record/Ziplines.json, and never
 * uploads or extracts files to disk.
 *
 * @module log_archive
 */

const EOCD_SIGNATURE = 0x06054b50;
const CENTRAL_SIGNATURE = 0x02014b50;
const LOCAL_SIGNATURE = 0x04034b50;
const ZIP64_SENTINEL = 0xffffffff;
const MAX_EOCD_SEARCH = 22 + 0xffff;

const LOG_ENTRY_RE = /(^|\/)cpp-algo\/debug\/maafw[^/]*\.log$/i;
const ZIPLINE_ENTRY_RE = /(^|\/)record\/Ziplines\.json$/i;

function basename(path) {
    return String(path || "").replaceAll("\\", "/").split("/").pop() || "";
}

function archiveIdentity(file) {
    const name = basename(file && (file.webkitRelativePath || file.name));
    const part = /^(.*)-part(\d+)\.zip$/i.exec(name);
    if (part) {
        return {key: part[1].toLowerCase(), label: part[1], part: Number(part[2])};
    }
    return {key: name.toLowerCase(), label: name.replace(/\.zip$/i, ""), part: null};
}

/**
 * Separate plain logs from ZIP archives and merge -partNN.zip files by prefix.
 * @param {Iterable<Blob & {name?:string,webkitRelativePath?:string}>} inputFiles
 * @returns {{plainFiles:Array<Object>,archiveGroups:Array<{key:string,label:string,files:Array<Object>}>}}
 */
export function groupLogInputFiles(inputFiles) {
    const plainFiles = [];
    const groups = new Map();
    for (const file of Array.from(inputFiles || [])) {
        const name = basename(file && (file.webkitRelativePath || file.name));
        if (!/\.zip$/i.test(name)) {
            plainFiles.push(file);
            continue;
        }
        const identity = archiveIdentity(file);
        if (identity.part !== null && identity.part < 1) {
            throw new Error(`${identity.label} 的分包编号必须从 part01 开始`);
        }
        let group = groups.get(identity.key);
        if (!group) {
            group = {key: identity.key, label: identity.label, files: []};
            groups.set(identity.key, group);
        }
        if (
            identity.part !== null &&
            group.files.some((entry) => archiveIdentity(entry).part === identity.part)
        ) {
            throw new Error(`${identity.label} 重复选择了 part${String(identity.part).padStart(2, "0")}`);
        }
        group.files.push(file);
    }

    for (const group of groups.values()) {
        group.files.sort((lhs, rhs) => {
            const left = archiveIdentity(lhs).part;
            const right = archiveIdentity(rhs).part;
            if (left === null || right === null) return basename(lhs.name).localeCompare(basename(rhs.name));
            return left - right;
        });
        const parts = group.files.map((file) => archiveIdentity(file).part).filter(Number.isFinite);
        if (parts.length) {
            const expected = Array.from({length: parts[parts.length - 1]}, (_, index) => index + 1);
            const missing = expected.filter((part) => !parts.includes(part));
            if (missing.length) {
                throw new Error(`${group.label} 缺少分包：${missing.map((part) => `part${String(part).padStart(2, "0")}`).join("、")}`);
            }
        }
    }
    return {plainFiles, archiveGroups: [...groups.values()]};
}

function findEocd(bytes) {
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    for (let offset = bytes.byteLength - 22; offset >= 0; offset -= 1) {
        if (
            view.getUint32(offset, true) === EOCD_SIGNATURE &&
            offset + 22 + view.getUint16(offset + 20, true) === bytes.byteLength
        ) {
            return offset;
        }
    }
    return -1;
}

function decodeName(bytes, flags) {
    const utf8 = (flags & 0x0800) !== 0;
    return new TextDecoder(utf8 ? "utf-8" : "windows-1252").decode(bytes).replaceAll("\\", "/");
}

/**
 * Open one ordinary ZIP archive. MaaEnd "partNN" bundles are independent ZIPs,
 * so each file is opened separately and merged at the logical group level.
 * @param {Blob} blob
 * @param {string} [sourceName='archive.zip']
 * @returns {Promise<{sourceName:string,entries:Array<Object>,readText:(entry:Object)=>Promise<string>}>}
 */
export async function openZipArchive(blob, sourceName = "archive.zip") {
    if (!blob || typeof blob.slice !== "function" || !Number.isFinite(blob.size)) {
        throw new Error(`${sourceName} 不是可读取的 ZIP 文件`);
    }
    const tailStart = Math.max(0, blob.size - MAX_EOCD_SEARCH);
    const tail = new Uint8Array(await blob.slice(tailStart).arrayBuffer());
    const eocdOffset = findEocd(tail);
    if (eocdOffset < 0) throw new Error(`${sourceName} 找不到 ZIP 中央目录`);
    const eocd = new DataView(tail.buffer, tail.byteOffset + eocdOffset, 22);
    const disk = eocd.getUint16(4, true);
    const centralDisk = eocd.getUint16(6, true);
    const entryCount = eocd.getUint16(10, true);
    const centralSize = eocd.getUint32(12, true);
    const centralOffset = eocd.getUint32(16, true);
    if (disk !== 0 || centralDisk !== 0) throw new Error(`${sourceName} 是传统跨卷 ZIP，当前仅支持 MaaEnd 独立 part 分包`);
    if ([centralSize, centralOffset].includes(ZIP64_SENTINEL) || entryCount === 0xffff) {
        throw new Error(`${sourceName} 使用 ZIP64，当前日志导入器暂不支持`);
    }
    if (centralOffset + centralSize > blob.size) throw new Error(`${sourceName} 的 ZIP 中央目录越界`);

    const bytes = new Uint8Array(await blob.slice(centralOffset, centralOffset + centralSize).arrayBuffer());
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const entries = [];
    let offset = 0;
    for (let index = 0; index < entryCount; index += 1) {
        if (offset + 46 > bytes.byteLength || view.getUint32(offset, true) !== CENTRAL_SIGNATURE) {
            throw new Error(`${sourceName} 的 ZIP 中央目录损坏（条目 ${index + 1}）`);
        }
        const flags = view.getUint16(offset + 8, true);
        const method = view.getUint16(offset + 10, true);
        const compressedSize = view.getUint32(offset + 20, true);
        const uncompressedSize = view.getUint32(offset + 24, true);
        const nameLength = view.getUint16(offset + 28, true);
        const extraLength = view.getUint16(offset + 30, true);
        const commentLength = view.getUint16(offset + 32, true);
        const localOffset = view.getUint32(offset + 42, true);
        const end = offset + 46 + nameLength + extraLength + commentLength;
        if (end > bytes.byteLength) throw new Error(`${sourceName} 的 ZIP 条目名称越界`);
        if ([compressedSize, uncompressedSize, localOffset].includes(ZIP64_SENTINEL)) {
            throw new Error(`${sourceName} 的条目使用 ZIP64，当前日志导入器暂不支持`);
        }
        const name = decodeName(bytes.subarray(offset + 46, offset + 46 + nameLength), flags);
        entries.push({name, flags, method, compressedSize, uncompressedSize, localOffset});
        offset = end;
    }

    const readText = async (entry) => {
        if (!entry || !entries.includes(entry)) throw new Error("ZIP 条目不属于当前压缩包");
        if ((entry.flags & 0x0001) !== 0) throw new Error(`${entry.name} 已加密，无法读取`);
        if (entry.method !== 0 && entry.method !== 8) throw new Error(`${entry.name} 使用不支持的压缩方法 ${entry.method}`);
        const localBytes = new Uint8Array(await blob.slice(entry.localOffset, entry.localOffset + 30).arrayBuffer());
        if (localBytes.byteLength < 30 || new DataView(localBytes.buffer).getUint32(0, true) !== LOCAL_SIGNATURE) {
            throw new Error(`${entry.name} 的 ZIP 本地文件头损坏`);
        }
        const localView = new DataView(localBytes.buffer);
        const dataOffset = entry.localOffset + 30 + localView.getUint16(26, true) + localView.getUint16(28, true);
        if (dataOffset + entry.compressedSize > blob.size) throw new Error(`${entry.name} 的压缩数据越界`);
        const compressed = blob.slice(dataOffset, dataOffset + entry.compressedSize);
        if (entry.method === 0) {
            const output = await compressed.arrayBuffer();
            if (output.byteLength !== entry.uncompressedSize) throw new Error(`${entry.name} 的解压长度不匹配`);
            return new TextDecoder().decode(output);
        }
        if (typeof DecompressionStream !== "function") {
            throw new Error("当前浏览器不支持 ZIP deflate 解压，请升级浏览器后重试");
        }
        const stream = compressed.stream().pipeThrough(new DecompressionStream("deflate-raw"));
        const output = await new Response(stream).arrayBuffer();
        if (output.byteLength !== entry.uncompressedSize) throw new Error(`${entry.name} 的解压长度不匹配`);
        return new TextDecoder().decode(output);
    };

    return {sourceName, entries, readText};
}

/** Keep only the two payload classes needed by log analysis. @param {Array<Object>} entries */
export function selectMaaEndArchiveEntries(entries) {
    const logs = [];
    const ziplineRecords = [];
    for (const entry of entries || []) {
        if (LOG_ENTRY_RE.test(entry.name)) logs.push(entry);
        else if (ZIPLINE_ENTRY_RE.test(entry.name)) ziplineRecords.push(entry);
    }
    return {logs, ziplineRecords};
}
