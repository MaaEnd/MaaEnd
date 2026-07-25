/**
 * WebGL rendering layer for MapNavigator (web migration, DESIGN.md §4).
 *
 * Draws three bottom-up layers into the shared drawing canvas:
 *   1. **basemap** — the zone's PNG as one textured quad spanning world-rect
 *      `[0,0]..[width,height]` (base px; PNGs are ≤2016² < MAX_TEXTURE_SIZE, one texture).
 *   2. **mesh** — the navmesh triangle overlay: height-gradient fill (navy→teal by
 *      world-Y) + deduped cyan wireframe, both zoom-scaled opacity.
 *   3. **dots** — optional gray walkable-dot points (per-triangle centroids).
 *
 * The renderer is purely presentational: every draw call takes a {@link Camera}
 * and reads *only* that camera + its own uploaded GL resources. It never touches
 * app state. World→screen is entirely the camera's job; this layer just folds the
 * camera's affine into a clip-space transform in the vertex shader.
 *
 * WebGL2 is used when available (`getContext('webgl2')`), else a WebGL1 fallback.
 * Shaders are GLSL ES 1.00 so a single source path works on both. The only
 * WebGL1-specific need is 32-bit element indices (zones exceed 65k verts), pulled
 * in via `OES_element_index_uint`.
 *
 * @module gl/renderer
 */

/** @typedef {import('../camera.js').Camera} Camera */

/**
 * Per-zone metadata the renderer needs.
 * @typedef {Object} ZoneMeta
 * @property {number} width  zone width in base px (world-rect max X)
 * @property {number} height zone height in base px (world-rect max Y)
 * @property {number[]} [transform] optional tier→base affine `[sx,tx,sy,ty]` (see §9); unused in MVP
 */

const TEX_VS = `
attribute vec2 a_pos;
attribute vec2 a_uv;
uniform vec2 u_scale;
uniform vec2 u_offset;
varying vec2 v_uv;
void main() {
  gl_Position = vec4(a_pos * u_scale + u_offset, 0.0, 1.0);
  v_uv = a_uv;
}
`;

const TEX_FS = `
precision mediump float;
uniform sampler2D u_tex;
varying vec2 v_uv;
void main() {
  gl_FragColor = texture2D(u_tex, v_uv);
}
`;

const FLAT_VS = `
attribute vec3 a_pos;
uniform vec2 u_scale;
uniform vec2 u_offset;
uniform float u_pointSize;
varying float v_height;
void main() {
  gl_Position = vec4(a_pos.xy * u_scale + u_offset, 0.0, 1.0);
  gl_PointSize = u_pointSize;
  v_height = a_pos.z;
}
`;

const FLAT_FS = `
precision mediump float;
varying float v_height;
uniform float u_min_height;
uniform float u_max_height;
uniform vec4 u_color;
uniform float u_use_height;
uniform float u_opacity;
void main() {
  if (u_use_height > 0.5 && u_max_height > u_min_height) {
    float h = clamp((v_height - u_min_height) / (u_max_height - u_min_height), 0.0, 1.0);
    vec3 lowColor = vec3(0.01, 0.08, 0.25);
    vec3 highColor = vec3(0.0, 0.8, 0.7);
    vec3 mixed = mix(lowColor, highColor, h);
    gl_FragColor = vec4(mixed, u_opacity);
  } else {
    gl_FragColor = vec4(u_color.rgb, u_color.a * u_opacity);
  }
}
`;

// Canvas clear color #000000 → linear-ish sRGB bytes / 255.
const CLEAR_R = 0.0;
const CLEAR_G = 0.0;
const CLEAR_B = 0.0;

// NMSH mesh-buffer layout (DESIGN.md §2.4), little-endian:
//   [0]  magic "NMSH"        (4 bytes)
//   [4]  u32 version (=1)
//   [8]  u32 vertexCount
//   [12] u32 triangleCount
//   [16] f32[vertexCount*3]  vertices (u, v, height) base px
//   [16 + vertexCount*12] u32[triangleCount*3] indices
const NMSH_HEADER_BYTES = 16;

// Matches the tk `max_points` walkable-dot stride cap (§2.4 / §4).
const DOT_STRIDE_CAP = 60000;

export class Renderer {
  /**
   * @param {HTMLCanvasElement} canvas the drawing surface (owns the GL context)
   */
  constructor(canvas) {
    /** @type {HTMLCanvasElement} */
    this.canvas = canvas;

    const gl = /** @type {WebGL2RenderingContext|WebGLRenderingContext|null} */ (
      canvas.getContext('webgl2') || canvas.getContext('webgl') || canvas.getContext('experimental-webgl')
    );
    if (!gl) throw new Error('Renderer: WebGL not available (webgl2/webgl both null)');
    /** @type {WebGL2RenderingContext|WebGLRenderingContext} */
    this.gl = gl;
    /** @type {boolean} true if a WebGL2 context was obtained */
    this.isWebGL2 = typeof WebGL2RenderingContext !== 'undefined' && gl instanceof WebGL2RenderingContext;

    // 32-bit indices: core in WebGL2, an extension in WebGL1. Meshes exceed 65k verts.
    if (!this.isWebGL2) {
      this._uintIndexExt = gl.getExtension('OES_element_index_uint') || null;
    }

    // --- programs ---------------------------------------------------------
    const texProgram = this._createProgram(TEX_VS, TEX_FS, { a_pos: 0, a_uv: 1 });
    this._texProg = {
      program: texProgram,
      u_scale: gl.getUniformLocation(texProgram, 'u_scale'),
      u_offset: gl.getUniformLocation(texProgram, 'u_offset'),
      u_tex: gl.getUniformLocation(texProgram, 'u_tex'),
    };
    const flatProgram = this._createProgram(FLAT_VS, FLAT_FS, { a_pos: 0 });
    this._flatProg = {
      program: flatProgram,
      u_scale: gl.getUniformLocation(flatProgram, 'u_scale'),
      u_offset: gl.getUniformLocation(flatProgram, 'u_offset'),
      u_color: gl.getUniformLocation(flatProgram, 'u_color'),
      u_pointSize: gl.getUniformLocation(flatProgram, 'u_pointSize'),
      u_min_height: gl.getUniformLocation(flatProgram, 'u_min_height'),
      u_max_height: gl.getUniformLocation(flatProgram, 'u_max_height'),
      u_use_height: gl.getUniformLocation(flatProgram, 'u_use_height'),
      u_opacity: gl.getUniformLocation(flatProgram, 'u_opacity'),
    };

    // --- basemap ----------------------------------------------------------
    /** @type {WebGLTexture|null} */
    this._tex = null;
    /** @type {ZoneMeta|null} */
    this._basemapMeta = null;
    this._quadVbo = gl.createBuffer(); // interleaved pos(2)+uv(2), rebuilt per basemap

    // --- mesh -------------------------------------------------------------
    /** @type {WebGLBuffer|null} */
    this._meshVbo = null;
    /** @type {WebGLBuffer|null} */
    this._meshIbo = null;
    this._meshVertexCount = 0;
    this._meshTriangleCount = 0;
    this._meshIndexCount = 0;
    /** @type {Float32Array|null} interleaved (u,v,height) — kept for dot centroids */
    this._meshVertices = null;
    /** @type {Uint32Array|null} */
    this._meshIndices = null;
    /** @type {ZoneMeta|null} */
    this._meshMeta = null;
    /** @type {WebGLBuffer|null} */
    this._meshWireIbo = null;
    this._meshWireIndexCount = 0;
    this._meshMinHeight = 0;
    this._meshMaxHeight = 0;

    // --- dots -------------------------------------------------------------
    /** @type {WebGLBuffer|null} */
    this._dotsVbo = null;
    this._dotsCount = 0;

    // --- visibility -------------------------------------------------------
    this.basemapVisible = true;
    this.meshVisible = true;
    this.dotsVisible = false;

    // --- rAF coalescing ---------------------------------------------------
    this._rafHandle = 0;
    /** @type {Camera|null} */
    this._pendingCamera = null;

    // --- viewport dims ----------------------------------------------------
    this.cssW = 1;
    this.cssH = 1;
    this.dpr = 1;

    // --- static GL state --------------------------------------------------
    gl.disable(gl.DEPTH_TEST);
    gl.disable(gl.CULL_FACE);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    gl.clearColor(CLEAR_R, CLEAR_G, CLEAR_B, 1.0);

    // Best-effort initial sizing so a render before the first resize() isn't degenerate.
    const w0 = canvas.clientWidth || canvas.width || 300;
    const h0 = canvas.clientHeight || canvas.height || 150;
    const dpr0 = (typeof window !== 'undefined' && window.devicePixelRatio) || 1;
    this.resize(w0, h0, dpr0);
  }

  // ==========================================================================
  // Public API
  // ==========================================================================

  /**
   * Upload a basemap image as the sole texture and build its world-rect quad.
   * Replaces (and deletes) any previous basemap texture.
   *
   * @param {HTMLImageElement|ImageBitmap|HTMLCanvasElement} image decoded image source
   * @param {ZoneMeta} zoneMeta zone dims (world-rect `[0,0]..[width,height]`)
   * @returns {void}
   */
  setBasemap(image, zoneMeta) {
    const gl = this.gl;
    this._deleteBasemap();

    const tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    // No Y-flip: base px is y-down with (0,0) at the image top-left, so texcoord
    // v = worldY/height already samples the correct image row (see §9 / renderer_tk nw-anchor).
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, image);
    // NPOT-safe params (base PNGs are non-power-of-two): clamp + linear, no mipmaps.
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    this._tex = tex;

    const w = (zoneMeta && zoneMeta.width) || image.width || /** @type {any} */ (image).naturalWidth || 1;
    const h = (zoneMeta && zoneMeta.height) || image.height || /** @type {any} */ (image).naturalHeight || 1;
    this._basemapMeta = { width: w, height: h, transform: (zoneMeta && zoneMeta.transform) || undefined };

    // TRIANGLE_STRIP quad, interleaved [posX, posY, u, v]; uv = pos / (w,h).
    const quad = new Float32Array([
      0, 0, 0, 0,
      w, 0, 1, 0,
      0, h, 0, 1,
      w, h, 1, 1,
    ]);
    gl.bindBuffer(gl.ARRAY_BUFFER, this._quadVbo);
    gl.bufferData(gl.ARRAY_BUFFER, quad, gl.STATIC_DRAW);
  }

  /**
   * Parse a §2.4 NMSH buffer and upload it as a VBO (interleaved u,v,height) + IBO.
   * Replaces (and deletes) any previous mesh. Keeps the parsed typed arrays for
   * centroid/dot computation.
   *
   * @param {ArrayBuffer} arrayBuffer raw NMSH bytes
   * @param {ZoneMeta} zoneMeta geometry-zone dims (used for coarse cull bounds)
   * @returns {void}
   */
  setMesh(arrayBuffer, zoneMeta) {
    const gl = this.gl;
    const dv = new DataView(arrayBuffer);

    const magic = String.fromCharCode(dv.getUint8(0), dv.getUint8(1), dv.getUint8(2), dv.getUint8(3));
    if (magic !== 'NMSH') throw new Error('setMesh: bad magic ' + JSON.stringify(magic) + ' (expected "NMSH")');
    const version = dv.getUint32(4, true);
    if (version !== 1) throw new Error('setMesh: unsupported NMSH version ' + version);
    const vertexCount = dv.getUint32(8, true);
    const triangleCount = dv.getUint32(12, true);

    const vertsFloatLen = vertexCount * 3;
    const indexFloatLen = triangleCount * 3;
    const indexByteOffset = NMSH_HEADER_BYTES + vertsFloatLen * 4; // 4-aligned: 16 + 12*vc
    const needBytes = indexByteOffset + indexFloatLen * 4;
    if (arrayBuffer.byteLength < needBytes) {
      throw new Error(
        'setMesh: truncated buffer, need ' + needBytes + ' bytes, got ' + arrayBuffer.byteLength,
      );
    }

    // Zero-copy views over the buffer. Offsets 16 and 16+12*vc are 4-aligned, so both
    // typed-array views are valid. Assumes little-endian (universal on WebGL targets).
    const vertices = new Float32Array(arrayBuffer, NMSH_HEADER_BYTES, vertsFloatLen);
    const indices = new Uint32Array(arrayBuffer, indexByteOffset, indexFloatLen);

    let minH = Infinity;
    let maxH = -Infinity;
    for (let i = 0; i < vertexCount; i += 1) {
      const h = vertices[i * 3 + 2];
      if (h < minH) minH = h;
      if (h > maxH) maxH = h;
    }
    this._meshMinHeight = minH === Infinity ? 0 : minH;
    this._meshMaxHeight = maxH === -Infinity ? 0 : maxH;

    // Boundary edges only: an edge owned by exactly one triangle is a real
    // walkable-area outline (outer contour, hole ring, plate seam). Interior
    // shared edges are skipped — the pack's plate re-triangulation covers big
    // areas with sliver fans whose interior edges render as solid noise.
    const V = vertexCount;
    /** @type {Map<number, number>} undirected edge key (min*V+max) -> min endpoint */
    const once = new Map();
    /** @type {Set<number>} keys seen at least twice (incl. non-manifold repeats) */
    const shared = new Set();
    const addEdge = (a, b) => {
      const key = a < b ? a * V + b : b * V + a;
      if (shared.has(key)) return;
      if (once.has(key)) {
        once.delete(key);
        shared.add(key);
      } else {
        once.set(key, a < b ? a : b);
      }
    };
    for (let i = 0; i < triangleCount; i += 1) {
      const b = i * 3;
      addEdge(indices[b], indices[b + 1]);
      addEdge(indices[b + 1], indices[b + 2]);
      addEdge(indices[b + 2], indices[b]);
    }
    const lines = new Uint32Array(once.size * 2);
    let li = 0;
    for (const [key, mn] of once) {
      lines[li] = mn;
      lines[li + 1] = key - mn * V;
      li += 2;
    }

    this._deleteMesh();

    this._meshVbo = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, this._meshVbo);
    gl.bufferData(gl.ARRAY_BUFFER, vertices, gl.STATIC_DRAW);

    this._meshIbo = gl.createBuffer();
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this._meshIbo);
    gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, indices, gl.STATIC_DRAW);

    this._meshWireIbo = gl.createBuffer();
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this._meshWireIbo);
    gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, lines, gl.STATIC_DRAW);
    this._meshWireIndexCount = lines.length;

    this._meshVertexCount = vertexCount;
    this._meshTriangleCount = triangleCount;
    this._meshIndexCount = indexFloatLen;
    this._meshVertices = vertices;
    this._meshIndices = indices;
    this._meshMeta = {
      width: (zoneMeta && zoneMeta.width) || 0,
      height: (zoneMeta && zoneMeta.height) || 0,
      transform: (zoneMeta && zoneMeta.transform) || undefined,
    };
  }

  /**
   * Provide or (re)compute the optional gray walkable dots. Pass a `Float32Array`
   * of interleaved `[x,y,...]` world-px points to set them explicitly, or call with
   * no argument to derive per-triangle centroids from the current mesh (mirroring
   * the tk `ceil(triangleCount/60000)` stride cap). No-ops cleanly with no mesh.
   *
   * @param {Float32Array} [data] explicit `[x0,y0,x1,y1,...]` world-px points
   * @returns {void}
   */
  setDots(data) {
    const gl = this.gl;
    let pts = null;

    if (data instanceof Float32Array) {
      pts = data;
    } else if (this._meshVertices && this._meshIndices && this._meshTriangleCount > 0) {
      const tc = this._meshTriangleCount;
      const stride = Math.max(1, Math.ceil(tc / DOT_STRIDE_CAP));
      const v = this._meshVertices;
      const idx = this._meshIndices;
      const n = Math.ceil(tc / stride);
      pts = new Float32Array(n * 2);
      let j = 0;
      for (let t = 0; t < tc; t += stride) {
        const b = t * 3;
        const i0 = idx[b] * 3;
        const i1 = idx[b + 1] * 3;
        const i2 = idx[b + 2] * 3;
        pts[j++] = (v[i0] + v[i1] + v[i2]) / 3; // u
        pts[j++] = (v[i0 + 1] + v[i1 + 1] + v[i2 + 1]) / 3; // v
      }
    }

    this._deleteDots();
    if (!pts || pts.length === 0) {
      this._dotsCount = 0;
      return;
    }
    this._dotsVbo = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, this._dotsVbo);
    gl.bufferData(gl.ARRAY_BUFFER, pts, gl.STATIC_DRAW);
    this._dotsCount = pts.length >> 1;
  }

  /**
   * Toggle the mesh overlay layer.
   * @param {boolean} visible
   * @returns {void}
   */
  setMeshVisible(visible) {
    this.meshVisible = !!visible;
  }

  /**
   * Toggle the basemap layer.
   * @param {boolean} visible
   * @returns {void}
   */
  setBasemapVisible(visible) {
    this.basemapVisible = !!visible;
  }

  /**
   * Toggle the walkable-dots layer.
   * @param {boolean} visible
   * @returns {void}
   */
  setDotsVisible(visible) {
    this.dotsVisible = !!visible;
  }

  /**
   * Resize the drawing buffer to match the CSS size at the given device-pixel
   * ratio, and update the GL viewport. Call on canvas/window resize and DPR change.
   *
   * @param {number} cssW canvas CSS width (px)
   * @param {number} cssH canvas CSS height (px)
   * @param {number} [dpr=1] device pixel ratio
   * @returns {void}
   */
  resize(cssW, cssH, dpr = 1) {
    const gl = this.gl;
    this.dpr = dpr > 0 ? dpr : 1;
    this.cssW = Math.max(1, cssW || 1);
    this.cssH = Math.max(1, cssH || 1);
    const bw = Math.max(1, Math.round(this.cssW * this.dpr));
    const bh = Math.max(1, Math.round(this.cssH * this.dpr));
    if (this.canvas.width !== bw) this.canvas.width = bw;
    if (this.canvas.height !== bh) this.canvas.height = bh;
    gl.viewport(0, 0, bw, bh);
  }

  /**
   * Schedule a single coalesced redraw on the next animation frame. Multiple calls
   * within a frame collapse to one {@link Renderer#render}. Stores the latest camera.
   *
   * @param {Camera} camera the shared view camera to render with
   * @returns {void}
   */
  requestRender(camera) {
    this._pendingCamera = camera;
    if (this._rafHandle) return;
    const raf =
      typeof requestAnimationFrame === 'function'
        ? requestAnimationFrame
        : /** @param {FrameRequestCallback} cb */ (cb) => setTimeout(() => cb(Date.now()), 16);
    this._rafHandle = raf(() => {
      this._rafHandle = 0;
      const cam = this._pendingCamera;
      this._pendingCamera = null;
      if (cam) this.render(cam);
    });
  }

  /**
   * Cancel a pending {@link Renderer#requestRender} frame, if any.
   * @returns {void}
   */
  cancelRender() {
    if (!this._rafHandle) return;
    if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(this._rafHandle);
    else clearTimeout(this._rafHandle);
    this._rafHandle = 0;
    this._pendingCamera = null;
  }

  /**
   * Draw one frame immediately: clear, then basemap → mesh → dots, bottom-up.
   * The world→clip transform is derived from `camera` + the current CSS size.
   *
   * @param {Camera} camera the shared view camera
   * @returns {void}
   */
  render(camera) {
    const gl = this.gl;
    gl.viewport(0, 0, this.canvas.width, this.canvas.height);
    gl.clearColor(CLEAR_R, CLEAR_G, CLEAR_B, 1.0);
    gl.clear(gl.COLOR_BUFFER_BIT);

    // Fold camera (world→canvas CSS px) then canvas→clip into one per-axis affine.
    // Derived from worldToCanvas so we stay decoupled from the camera's field names.
    //   canvas: cx = wx*sx + ax ; clip: clipX = 2*cx/cssW - 1, clipY = 1 - 2*cy/cssH
    // → clip = pos * uScale + uOffset. dpr-independent (clip is a viewport ratio).
    const [ax, ay] = camera.worldToCanvas(0, 0);
    const [bx, by] = camera.worldToCanvas(1, 1);
    const sx = bx - ax;
    const sy = by - ay;
    const uScaleX = (2 * sx) / this.cssW;
    const uScaleY = (-2 * sy) / this.cssH;
    const uOffX = (2 * ax) / this.cssW - 1;
    const uOffY = 1 - (2 * ay) / this.cssH;

    const vb = this._visibleWorldBounds(camera);

    // 1) basemap (opaque)
    if (this.basemapVisible && this._tex) {
      const m = this._basemapMeta;
      if (!m || this._boundsIntersect(vb, 0, 0, m.width, m.height)) {
        this._drawBasemap(uScaleX, uScaleY, uOffX, uOffY);
      }
    }

    // 2) mesh triangle overlay (translucent red)
    if (this.meshVisible && this._meshIbo && this._meshIndexCount > 0) {
      const m = this._meshMeta;
      const cullable = m && m.width > 0 && m.height > 0;
      if (!cullable || this._boundsIntersect(vb, 0, 0, m.width, m.height)) {
        this._drawMesh(uScaleX, uScaleY, uOffX, uOffY, camera.viewScale);
      }
    }

    // 3) walkable dots (gray)
    if (this.dotsVisible && this._dotsVbo && this._dotsCount > 0) {
      this._drawDots(uScaleX, uScaleY, uOffX, uOffY);
    }
  }

  /**
   * Release all GL resources (textures, buffers, programs) and cancel pending frames.
   * @returns {void}
   */
  dispose() {
    const gl = this.gl;
    this.cancelRender();
    this._deleteBasemap();
    this._deleteMesh();
    this._deleteDots();
    if (this._quadVbo) {
      gl.deleteBuffer(this._quadVbo);
      this._quadVbo = null;
    }
    if (this._texProg && this._texProg.program) gl.deleteProgram(this._texProg.program);
    if (this._flatProg && this._flatProg.program) gl.deleteProgram(this._flatProg.program);
  }

  // ==========================================================================
  // Private draw helpers
  // ==========================================================================

  /**
   * @param {number} sx @param {number} sy @param {number} ox @param {number} oy
   * @returns {void}
   */
  _drawBasemap(sx, sy, ox, oy) {
    const gl = this.gl;
    const p = this._texProg;
    gl.useProgram(p.program);
    gl.disable(gl.BLEND); // base layer is opaque
    gl.uniform2f(p.u_scale, sx, sy);
    gl.uniform2f(p.u_offset, ox, oy);
    gl.uniform1i(p.u_tex, 0);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, this._tex);
    gl.bindBuffer(gl.ARRAY_BUFFER, this._quadVbo);
    gl.enableVertexAttribArray(0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 16, 0); // pos
    gl.vertexAttribPointer(1, 2, gl.FLOAT, false, 16, 8); // uv
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
  }

  /**
   * @param {number} sx @param {number} sy @param {number} ox @param {number} oy
   * @param {number} viewScale
   * @returns {void}
   */
  _drawMesh(sx, sy, ox, oy, viewScale = 1.0) {
    const gl = this.gl;
    const p = this._flatProg;
    gl.useProgram(p.program);
    gl.enable(gl.BLEND);
    gl.uniform2f(p.u_scale, sx, sy);
    gl.uniform2f(p.u_offset, ox, oy);

    // Opacity follows zoom (power curve, clamped) so the overlay reads at high zoom
    // without washing out the basemap when zoomed far out. Lines are boundary-only
    // (sparse), so they can afford to be crisp.
    const meshOpacity = Math.max(0.04, Math.min(0.15, 0.04 + Math.pow(viewScale, 1.5) * 0.06));
    const wireOpacity = Math.max(0.25, Math.min(0.8, 0.25 + Math.pow(viewScale, 1.5) * 0.2));

    gl.uniform1f(p.u_use_height, 1.0);
    gl.uniform1f(p.u_min_height, this._meshMinHeight);
    gl.uniform1f(p.u_max_height, this._meshMaxHeight);
    gl.uniform4f(p.u_color, 1.0, 0.0, 0.0, 1.0); // fallback when the height range is degenerate
    gl.uniform1f(p.u_opacity, meshOpacity);
    gl.uniform1f(p.u_pointSize, 1.0);

    gl.bindBuffer(gl.ARRAY_BUFFER, this._meshVbo);
    gl.enableVertexAttribArray(0);
    gl.disableVertexAttribArray(1); // flat program has no a_uv; avoid a stale attrib
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 12, 0); // interleaved (u, v, height)
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this._meshIbo);
    gl.drawElements(gl.TRIANGLES, this._meshIndexCount, gl.UNSIGNED_INT, 0);

    if (this._meshWireIbo && this._meshWireIndexCount > 0) {
      gl.uniform1f(p.u_use_height, 0.0);
      gl.uniform4f(p.u_color, 0.0, 1.0, 0.9, wireOpacity);
      gl.uniform1f(p.u_opacity, 1.0);
      gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this._meshWireIbo);
      gl.drawElements(gl.LINES, this._meshWireIndexCount, gl.UNSIGNED_INT, 0);
    }
  }

  /**
   * @param {number} sx @param {number} sy @param {number} ox @param {number} oy
   * @returns {void}
   */
  _drawDots(sx, sy, ox, oy) {
    const gl = this.gl;
    const p = this._flatProg;
    gl.useProgram(p.program);
    gl.enable(gl.BLEND);
    gl.uniform2f(p.u_scale, sx, sy);
    gl.uniform2f(p.u_offset, ox, oy);
    gl.uniform4f(p.u_color, 0.58, 0.64, 0.72, 0.85); // slate gray (#94a3b8-ish)
    gl.uniform1f(p.u_pointSize, Math.max(1, 2 * this.dpr));
    gl.bindBuffer(gl.ARRAY_BUFFER, this._dotsVbo);
    gl.enableVertexAttribArray(0);
    gl.disableVertexAttribArray(1);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 8, 0); // tightly packed x,y
    gl.drawArrays(gl.POINTS, 0, this._dotsCount);
  }

  // ==========================================================================
  // Private utilities
  // ==========================================================================

  /**
   * World-space rect currently visible in the canvas, as `[minX,minY,maxX,maxY]`.
   * @param {Camera} camera
   * @returns {[number, number, number, number]}
   */
  _visibleWorldBounds(camera) {
    const [x0, y0] = camera.canvasToWorld(0, 0);
    const [x1, y1] = camera.canvasToWorld(this.cssW, this.cssH);
    return [Math.min(x0, x1), Math.min(y0, y1), Math.max(x0, x1), Math.max(y0, y1)];
  }

  /**
   * Axis-aligned overlap test between the visible bounds and rect `[bx0,by0,bx1,by1]`.
   * @param {number[]} a visible bounds `[minX,minY,maxX,maxY]`
   * @param {number} bx0 @param {number} by0 @param {number} bx1 @param {number} by1
   * @returns {boolean}
   */
  _boundsIntersect(a, bx0, by0, bx1, by1) {
    return a[0] <= bx1 && bx0 <= a[2] && a[1] <= by1 && by0 <= a[3];
  }

  /**
   * Delete the basemap texture (keeps the reusable quad VBO).
   * @returns {void}
   */
  _deleteBasemap() {
    const gl = this.gl;
    if (this._tex) {
      gl.deleteTexture(this._tex);
      this._tex = null;
    }
    this._basemapMeta = null;
  }

  /**
   * Delete mesh GL buffers and drop the retained typed arrays.
   * @returns {void}
   */
  _deleteMesh() {
    const gl = this.gl;
    if (this._meshVbo) {
      gl.deleteBuffer(this._meshVbo);
      this._meshVbo = null;
    }
    if (this._meshIbo) {
      gl.deleteBuffer(this._meshIbo);
      this._meshIbo = null;
    }
    if (this._meshWireIbo) {
      gl.deleteBuffer(this._meshWireIbo);
      this._meshWireIbo = null;
    }
    this._meshWireIndexCount = 0;
    this._meshVertexCount = 0;
    this._meshTriangleCount = 0;
    this._meshIndexCount = 0;
    this._meshVertices = null;
    this._meshIndices = null;
    this._meshMeta = null;
  }

  /**
   * Delete the dots GL buffer.
   * @returns {void}
   */
  _deleteDots() {
    const gl = this.gl;
    if (this._dotsVbo) {
      gl.deleteBuffer(this._dotsVbo);
      this._dotsVbo = null;
    }
    this._dotsCount = 0;
  }

  /**
   * Compile + link a program, binding attribute locations before linking.
   * @param {string} vsrc vertex shader source (GLSL ES 1.00)
   * @param {string} fsrc fragment shader source
   * @param {Object<string, number>} attribs attribute name → location
   * @returns {WebGLProgram}
   */
  _createProgram(vsrc, fsrc, attribs) {
    const gl = this.gl;
    const vs = this._compileShader(gl.VERTEX_SHADER, vsrc);
    const fs = this._compileShader(gl.FRAGMENT_SHADER, fsrc);
    const prog = gl.createProgram();
    gl.attachShader(prog, vs);
    gl.attachShader(prog, fs);
    for (const name in attribs) {
      if (Object.prototype.hasOwnProperty.call(attribs, name)) {
        gl.bindAttribLocation(prog, attribs[name], name);
      }
    }
    gl.linkProgram(prog);
    // Shaders are retained by the linked program; safe to flag for deletion now.
    gl.deleteShader(vs);
    gl.deleteShader(fs);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      const info = gl.getProgramInfoLog(prog);
      gl.deleteProgram(prog);
      throw new Error('Renderer: program link failed: ' + info);
    }
    return prog;
  }

  /**
   * Compile a single shader.
   * @param {number} type `gl.VERTEX_SHADER` or `gl.FRAGMENT_SHADER`
   * @param {string} src shader source
   * @returns {WebGLShader}
   */
  _compileShader(type, src) {
    const gl = this.gl;
    const sh = gl.createShader(type);
    gl.shaderSource(sh, src);
    gl.compileShader(sh);
    if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
      const info = gl.getShaderInfoLog(sh);
      gl.deleteShader(sh);
      throw new Error('Renderer: shader compile failed: ' + info);
    }
    return sh;
  }
}
