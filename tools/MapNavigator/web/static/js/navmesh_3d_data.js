const NMSH_HEADER_BYTES = 16;
const NMSH_VERSION = 1;

/**
 * Parse the little-endian NMSH payload shared by the 2D and 3D renderers.
 * The returned typed arrays are zero-copy views over the source buffer.
 *
 * @param {ArrayBuffer} buffer
 * @returns {{
 *   vertexCount:number,
 *   triangleCount:number,
 *   vertices:Float32Array,
 *   indices:Uint32Array,
 *   bounds:{minU:number,maxU:number,minV:number,maxV:number,minHeight:number,maxHeight:number}
 * }}
 */
export function parseNmsh(buffer) {
    if (!(buffer instanceof ArrayBuffer)) {
        throw new TypeError("parseNmsh: expected an ArrayBuffer");
    }
    if (buffer.byteLength < NMSH_HEADER_BYTES) {
        throw new Error(`parseNmsh: truncated header, got ${buffer.byteLength} bytes`);
    }

    const view = new DataView(buffer);
    const magic = String.fromCharCode(view.getUint8(0), view.getUint8(1), view.getUint8(2), view.getUint8(3));
    if (magic !== "NMSH") {
        throw new Error(`parseNmsh: bad magic ${JSON.stringify(magic)}`);
    }

    const version = view.getUint32(4, true);
    if (version !== NMSH_VERSION) {
        throw new Error(`parseNmsh: unsupported version ${version}`);
    }

    const vertexCount = view.getUint32(8, true);
    const triangleCount = view.getUint32(12, true);
    const vertexValueCount = vertexCount * 3;
    const indexValueCount = triangleCount * 3;
    const indexByteOffset = NMSH_HEADER_BYTES + vertexValueCount * Float32Array.BYTES_PER_ELEMENT;
    const requiredBytes = indexByteOffset + indexValueCount * Uint32Array.BYTES_PER_ELEMENT;
    if (buffer.byteLength < requiredBytes) {
        throw new Error(`parseNmsh: truncated buffer, need ${requiredBytes} bytes, got ${buffer.byteLength}`);
    }

    const vertices = new Float32Array(buffer, NMSH_HEADER_BYTES, vertexValueCount);
    const indices = new Uint32Array(buffer, indexByteOffset, indexValueCount);
    let minU = Infinity;
    let maxU = -Infinity;
    let minV = Infinity;
    let maxV = -Infinity;
    let minHeight = Infinity;
    let maxHeight = -Infinity;

    for (let i = 0; i < vertexCount; i += 1) {
        const offset = i * 3;
        const u = vertices[offset];
        const v = vertices[offset + 1];
        const height = vertices[offset + 2];
        if (!Number.isFinite(u) || !Number.isFinite(v) || !Number.isFinite(height)) {
            throw new Error(`parseNmsh: vertex ${i} contains a non-finite value`);
        }
        minU = Math.min(minU, u);
        maxU = Math.max(maxU, u);
        minV = Math.min(minV, v);
        maxV = Math.max(maxV, v);
        minHeight = Math.min(minHeight, height);
        maxHeight = Math.max(maxHeight, height);
    }

    for (let i = 0; i < indices.length; i += 1) {
        if (indices[i] >= vertexCount) {
            throw new Error(`parseNmsh: index ${indices[i]} is outside ${vertexCount} vertices`);
        }
    }

    const emptyValue = 0;
    return {
        vertexCount,
        triangleCount,
        vertices,
        indices,
        bounds: {
            minU: minU === Infinity ? emptyValue : minU,
            maxU: maxU === -Infinity ? emptyValue : maxU,
            minV: minV === Infinity ? emptyValue : minV,
            maxV: maxV === -Infinity ? emptyValue : maxV,
            minHeight: minHeight === Infinity ? emptyValue : minHeight,
            maxHeight: maxHeight === -Infinity ? emptyValue : maxHeight,
        },
    };
}

/** Return the edges owned by exactly one triangle as index pairs. */
export function buildBoundaryEdgeIndices(indices, vertexCount) {
    const once = new Map();
    const shared = new Set();
    const addEdge = (a, b) => {
        const min = Math.min(a, b);
        const max = Math.max(a, b);
        const key = min * vertexCount + max;
        if (shared.has(key)) return;
        if (once.has(key)) {
            once.delete(key);
            shared.add(key);
        } else {
            once.set(key, [min, max]);
        }
    };

    for (let i = 0; i < indices.length; i += 3) {
        addEdge(indices[i], indices[i + 1]);
        addEdge(indices[i + 1], indices[i + 2]);
        addEdge(indices[i + 2], indices[i]);
    }

    const result = new Uint32Array(once.size * 2);
    let offset = 0;
    for (const [a, b] of once.values()) {
        result[offset] = a;
        result[offset + 1] = b;
        offset += 2;
    }
    return result;
}
