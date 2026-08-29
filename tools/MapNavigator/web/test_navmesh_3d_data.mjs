import assert from "node:assert/strict";
import test from "node:test";

import {buildBoundaryEdgeIndices, parseNmsh} from "./static/js/navmesh_3d_data.js";

function makeNmsh(vertices, indices) {
    const vertexCount = vertices.length / 3;
    const triangleCount = indices.length / 3;
    const buffer = new ArrayBuffer(16 + vertices.length * 4 + indices.length * 4);
    const view = new DataView(buffer);
    for (const [index, char] of [..."NMSH"].entries()) view.setUint8(index, char.charCodeAt(0));
    view.setUint32(4, 1, true);
    view.setUint32(8, vertexCount, true);
    view.setUint32(12, triangleCount, true);
    new Float32Array(buffer, 16, vertices.length).set(vertices);
    new Uint32Array(buffer, 16 + vertices.length * 4, indices.length).set(indices);
    return buffer;
}

test("parseNmsh returns zero-copy geometry and full 3D bounds", () => {
    const buffer = makeNmsh(
        [
            10, 20, 3,
            30, 15, 8,
            22, 40, -2,
        ],
        [0, 1, 2],
    );

    const parsed = parseNmsh(buffer);
    assert.equal(parsed.vertexCount, 3);
    assert.equal(parsed.triangleCount, 1);
    assert.deepEqual([...parsed.indices], [0, 1, 2]);
    assert.deepEqual(parsed.bounds, {
        minU: 10,
        maxU: 30,
        minV: 15,
        maxV: 40,
        minHeight: -2,
        maxHeight: 8,
    });
    assert.equal(parsed.vertices.buffer, buffer);
    assert.equal(parsed.indices.buffer, buffer);
});

test("parseNmsh rejects invalid headers and truncated payloads", () => {
    assert.throws(() => parseNmsh(new ArrayBuffer(8)), /truncated header/);

    const badMagic = makeNmsh([0, 0, 0], []);
    new DataView(badMagic).setUint8(0, "X".charCodeAt(0));
    assert.throws(() => parseNmsh(badMagic), /bad magic/);

    const truncated = makeNmsh([0, 0, 0, 1, 0, 0, 0, 1, 0], [0, 1, 2]).slice(0, -4);
    assert.throws(() => parseNmsh(truncated), /truncated buffer/);
});

test("parseNmsh rejects non-finite vertices and invalid indices", () => {
    const invalidVertex = makeNmsh([0, 0, 0, 1, 0, Number.NaN, 0, 1, 0], [0, 1, 2]);
    assert.throws(() => parseNmsh(invalidVertex), /non-finite/);

    const invalidIndex = makeNmsh([0, 0, 0, 1, 0, 0, 0, 1, 0], [0, 1, 3]);
    assert.throws(() => parseNmsh(invalidIndex), /outside 3 vertices/);
});

test("buildBoundaryEdgeIndices removes shared triangle edges", () => {
    const edges = buildBoundaryEdgeIndices(new Uint32Array([0, 1, 2, 2, 1, 3]), 4);
    const normalized = [];
    for (let i = 0; i < edges.length; i += 2) normalized.push(`${edges[i]}:${edges[i + 1]}`);
    assert.deepEqual(normalized.sort(), ["0:1", "0:2", "1:3", "2:3"]);
});
