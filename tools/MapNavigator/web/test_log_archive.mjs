import assert from "node:assert/strict";
import test from "node:test";
import {deflateRawSync} from "node:zlib";

import {groupLogInputFiles, openZipArchive, selectMaaEndArchiveEntries} from "./static/js/log_archive.js";

function namedBlob(name, bytes = new Uint8Array()) {
  const blob = new Blob([bytes]);
  Object.defineProperty(blob, "name", {value: name});
  return blob;
}

function makeZip(entries) {
  const locals = [];
  const centrals = [];
  let localOffset = 0;
  for (const entry of entries) {
    const name = Buffer.from(entry.name, "utf8");
    const raw = Buffer.from(entry.text, "utf8");
    const method = entry.method ?? 8;
    const compressed = method === 8 ? deflateRawSync(raw) : raw;

    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(0x0800, 6);
    local.writeUInt16LE(method, 8);
    local.writeUInt32LE(compressed.length, 18);
    local.writeUInt32LE(raw.length, 22);
    local.writeUInt16LE(name.length, 26);
    locals.push(local, name, compressed);

    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(0x0800, 8);
    central.writeUInt16LE(method, 10);
    central.writeUInt32LE(compressed.length, 20);
    central.writeUInt32LE(raw.length, 24);
    central.writeUInt16LE(name.length, 28);
    central.writeUInt32LE(localOffset, 42);
    centrals.push(central, name);
    localOffset += local.length + name.length + compressed.length;
  }

  const centralBytes = Buffer.concat(centrals);
  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0);
  eocd.writeUInt16LE(entries.length, 8);
  eocd.writeUInt16LE(entries.length, 10);
  eocd.writeUInt32LE(centralBytes.length, 12);
  eocd.writeUInt32LE(localOffset, 16);
  return namedBlob("bundle.zip", Buffer.concat([...locals, centralBytes, eocd]));
}

test("groups independent MaaEnd part ZIPs and detects a missing part", () => {
  const part02 = namedBlob("MaaEnd-logs-v2-part02.zip");
  const part01 = namedBlob("MaaEnd-logs-v2-part01.zip");
  const plain = namedBlob("maafw.log");
  const grouped = groupLogInputFiles([part02, plain, part01]);

  assert.deepEqual(grouped.plainFiles, [plain]);
  assert.equal(grouped.archiveGroups.length, 1);
  assert.deepEqual(grouped.archiveGroups[0].files, [part01, part02]);
  assert.throws(() => groupLogInputFiles([part02]), /缺少分包.*part01/);
  assert.throws(() => groupLogInputFiles([namedBlob("MaaEnd-logs-v2-part00.zip")]), /必须从 part01 开始/);
});

test("reads stored and deflated relevant entries while ignoring unrelated files", async () => {
  const archive = await openZipArchive(
    makeZip([
      {name: "cpp-algo/debug/maafw.log", text: "current log", method: 0},
      {name: "cpp-algo/debug/maafw.bak.2026.08.26.log", text: "rolled log", method: 8},
      {name: "record/Ziplines.json", text: '{"maps":[]}', method: 8},
      {name: "on_error/screenshot.png", text: "not read", method: 8},
    ]),
    "part02.zip",
  );
  const selected = selectMaaEndArchiveEntries(archive.entries);

  assert.equal(selected.logs.length, 2);
  assert.equal(selected.ziplineRecords.length, 1);
  assert.equal(await archive.readText(selected.logs[0]), "current log");
  assert.equal(await archive.readText(selected.logs[1]), "rolled log");
  assert.deepEqual(JSON.parse(await archive.readText(selected.ziplineRecords[0])), {maps: []});
  assert.ok(!archive.entries.some((entry) => selected.logs.includes(entry) && entry.name.includes("screenshot")));
});
