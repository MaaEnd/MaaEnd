import * as THREE from "three";
import {OrbitControls} from "three/addons/controls/OrbitControls.js";

import {buildBoundaryEdgeIndices, parseNmsh} from "./navmesh_3d_data.js";

const LOW_COLOR = [0.01, 0.06, 0.18];
const MID_COLOR = [0.0, 0.36, 0.26];
const HIGH_COLOR = [0.85, 0.28, 0.02];
const MOVEMENT_CODES = new Set(["KeyW", "KeyA", "KeyS", "KeyD"]);
const CONTROL_CODES = new Set(["ControlLeft", "ControlRight"]);
const FREE_LOOK_SENSITIVITY = 0.003;
const MAX_FREE_LOOK_PITCH = Math.PI / 2 - 0.01;
const HEIGHT_CONTRAST = 3.2;
const PATH_RADIUS = 0.1;
const DIAGNOSTIC_RADIUS = 0.045;
const PATH_LIFT = 0.02;
const PATH_COLOR = 0xff4fd8;

function writeHeightColor(target, offset, value) {
  const t = Math.max(0, Math.min(1, value));
  const from = t < 0.5 ? LOW_COLOR : MID_COLOR;
  const to = t < 0.5 ? MID_COLOR : HIGH_COLOR;
  const localT = t < 0.5 ? t * 2 : (t - 0.5) * 2;
  target[offset] = from[0] + (to[0] - from[0]) * localT;
  target[offset + 1] = from[1] + (to[1] - from[1]) * localT;
  target[offset + 2] = from[2] + (to[2] - from[2]) * localT;
}

export class ThreeNavmeshView {
  /**
   * @param {{canvas:HTMLCanvasElement,onPick:(point:{u:number,v:number,height:number})=>void}} options
   */
  constructor({canvas, onPick}) {
    this.canvas = canvas;
    this.onPick = onPick;
    this.scene = new THREE.Scene();
    this.scene.background = new THREE.Color(0x080d12);

    this.camera = new THREE.PerspectiveCamera(45, 1, 0.1, 10000);
    this.renderer = new THREE.WebGLRenderer({canvas, antialias: true, alpha: false});
    this.renderer.outputColorSpace = THREE.SRGBColorSpace;
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));

    this.controls = new OrbitControls(this.camera, canvas);
    this.controls.enableDamping = false;
    this.controls.screenSpacePanning = true;
    this.controls.mouseButtons.LEFT = null;
    this.controls.mouseButtons.MIDDLE = THREE.MOUSE.PAN;
    this.controls.mouseButtons.RIGHT = null;
    this.controls.addEventListener("change", () => this.render());

    this.navigationMode = "free";
    this.movementSpeedMultiplier = 1;
    this.mesh = null;
    this.wireMesh = null;
    this.grid = null;
    this.marker = null;
    this.liveMarker = null;
    this.liveHeading = null;
    this.routeLine = null;
    this.livePathLine = null;
    this.diagnosticLines = [];
    this.diagnosticMarkers = [];
    this.vertexHeights = null;
    this.meshMinHeight = 0;
    this.meshMaxHeight = 0;
    this.heightFocus = null;
    this.lastHeightColorUpdate = 0;
    this.meshCenter = {u: 0, v: 0, height: 0};
    this.focusTarget = new THREE.Vector3();
    this.meshRadius = 1;
    this.visible = false;
    this.meshLayerVisible = true;
    this.pointerDown = null;
    this.freeLookPointer = null;
    this.raycaster = new THREE.Raycaster();
    this.pointer = new THREE.Vector2();
    this.lookEuler = new THREE.Euler(0, 0, 0, "YXZ");
    this.lookForward = new THREE.Vector3();
    this.movementKeys = new Set();
    this.spacePressed = false;
    this.movementFrame = 0;
    this.lastMovementTime = null;
    this.movementForward = new THREE.Vector3();
    this.movementRight = new THREE.Vector3();
    this.movementDelta = new THREE.Vector3();
    this.worldUp = new THREE.Vector3(0, 1, 0);
    this._movementFrameBound = (timestamp) => this._onMovementFrame(timestamp);

    canvas.addEventListener("pointerdown", (event) => this._onPointerDown(event));
    canvas.addEventListener("pointermove", (event) => this._onPointerMove(event));
    canvas.addEventListener("pointerup", (event) => this._onPointerUp(event));
    canvas.addEventListener("click", (event) => this._onClick(event));
    canvas.addEventListener("pointercancel", (event) => {
      this.pointerDown = null;
      this._endFreeLook(event.pointerId);
    });
    canvas.addEventListener("contextmenu", (event) => event.preventDefault());
    window.addEventListener("blur", () => {
      this._stopMovement();
      this._endFreeLook();
    });
  }

  /** @param {ArrayBuffer} buffer */
  setMesh(buffer) {
    const parsed = parseNmsh(buffer);
    this.clearMesh();

    const {minU, maxU, minV, maxV, minHeight, maxHeight} = parsed.bounds;
    this.meshMinHeight = minHeight;
    this.meshMaxHeight = maxHeight;
    this.meshCenter = {
      u: (minU + maxU) / 2,
      v: (minV + maxV) / 2,
      height: (minHeight + maxHeight) / 2,
    };

    const positions = new Float32Array(parsed.vertices.length);
    const colors = new Float32Array(parsed.vertices.length);
    this.vertexHeights = new Float32Array(parsed.vertexCount);
    const heightSpan = Math.max(1e-6, maxHeight - minHeight);
    const heightMid = (minHeight + maxHeight) / 2;
    for (let i = 0; i < parsed.vertexCount; i += 1) {
      const offset = i * 3;
      positions[offset] = parsed.vertices[offset] - this.meshCenter.u;
      positions[offset + 1] = parsed.vertices[offset + 2] - this.meshCenter.height;
      positions[offset + 2] = parsed.vertices[offset + 1] - this.meshCenter.v;
      this.vertexHeights[i] = parsed.vertices[offset + 2];
      const contrasted = 0.5 + ((parsed.vertices[offset + 2] - heightMid) / heightSpan) * HEIGHT_CONTRAST;
      writeHeightColor(colors, offset, contrasted);
    }

    let nearestTriangleDistance = Infinity;
    for (let i = 0; i < parsed.indices.length; i += 3) {
      const a = parsed.indices[i] * 3;
      const b = parsed.indices[i + 1] * 3;
      const c = parsed.indices[i + 2] * 3;
      const x = (positions[a] + positions[b] + positions[c]) / 3;
      const y = (positions[a + 1] + positions[b + 1] + positions[c + 1]) / 3;
      const z = (positions[a + 2] + positions[b + 2] + positions[c + 2]) / 3;
      const distance = x * x + y * y + z * z;
      if (distance < nearestTriangleDistance) {
        nearestTriangleDistance = distance;
        this.focusTarget.set(x, y, z);
      }
    }

    const geometry = new THREE.BufferGeometry();
    geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
    geometry.setAttribute("color", new THREE.BufferAttribute(colors, 3));
    geometry.setIndex(new THREE.BufferAttribute(parsed.indices, 1));
    geometry.computeVertexNormals();
    geometry.computeBoundingSphere();

    const material = new THREE.MeshBasicMaterial({
      vertexColors: true,
      side: THREE.DoubleSide,
      polygonOffset: true,
      polygonOffsetFactor: 1,
      polygonOffsetUnits: 1,
    });
    this.mesh = new THREE.Mesh(geometry, material);
    this.scene.add(this.mesh);

    const wireGeometry = new THREE.BufferGeometry();
    wireGeometry.setAttribute("position", geometry.getAttribute("position"));
    wireGeometry.setIndex(new THREE.BufferAttribute(buildBoundaryEdgeIndices(parsed.indices, parsed.vertexCount), 1));
    const wireMaterial = new THREE.LineBasicMaterial({
      color: 0xc4f1ec,
      transparent: true,
      opacity: 0.42,
      depthWrite: false,
    });
    this.wireMesh = new THREE.LineSegments(wireGeometry, wireMaterial);
    this.scene.add(this.wireMesh);

    let radiusSquared = 1;
    for (let i = 0; i < positions.length; i += 3) {
      const dx = positions[i] - this.focusTarget.x;
      const dy = positions[i + 1] - this.focusTarget.y;
      const dz = positions[i + 2] - this.focusTarget.z;
      radiusSquared = Math.max(radiusSquared, dx * dx + dy * dy + dz * dz);
    }
    this.meshRadius = Math.sqrt(radiusSquared);
    const gridSize = Math.max(maxU - minU, maxV - minV, 10) * 1.1;
    this.grid = new THREE.GridHelper(gridSize, 20, 0x35505b, 0x18262d);
    this.grid.position.y = minHeight - this.meshCenter.height - Math.max(0.5, this.meshRadius * 0.001);
    this.scene.add(this.grid);

    const markerGeometry = new THREE.SphereGeometry(Math.max(0.18, this.meshRadius * 0.0012), 16, 10);
    const markerMaterial = new THREE.MeshBasicMaterial({color: 0xffbd4a, depthTest: false});
    this.marker = new THREE.Mesh(markerGeometry, markerMaterial);
    this.marker.visible = false;
    this.marker.renderOrder = 10;
    this.scene.add(this.marker);
    const liveMaterial = new THREE.MeshBasicMaterial({color: 0x38bdf8, depthTest: false});
    this.liveMarker = new THREE.Mesh(markerGeometry.clone(), liveMaterial);
    this.liveMarker.visible = false;
    this.liveMarker.renderOrder = 11;
    this.scene.add(this.liveMarker);
    const headingGeometry = new THREE.BufferGeometry();
    headingGeometry.setAttribute("position", new THREE.Float32BufferAttribute([0, 0, 0, 0, 0, 0], 3));
    this.liveHeading = new THREE.Line(
      headingGeometry,
      new THREE.LineBasicMaterial({color: 0xffffff, depthTest: false}),
    );
    this.liveHeading.visible = false;
    this.liveHeading.renderOrder = 12;
    this.scene.add(this.liveHeading);

    this.setMeshVisible(this.meshLayerVisible);
    this.fitView();
  }

  clearMesh() {
    this._stopMovement();
    if (this.mesh) {
      this.scene.remove(this.mesh);
      this._disposeObject(this.mesh);
    }
    if (this.wireMesh) {
      this.scene.remove(this.wireMesh);
      this._disposeObject(this.wireMesh);
    }
    if (this.grid) {
      this.scene.remove(this.grid);
      this._disposeObject(this.grid);
    }
    if (this.marker) {
      this.scene.remove(this.marker);
      this._disposeObject(this.marker);
    }
    if (this.liveMarker) {
      this.scene.remove(this.liveMarker);
      this._disposeObject(this.liveMarker);
    }
    if (this.liveHeading) {
      this.scene.remove(this.liveHeading);
      this._disposeObject(this.liveHeading);
    }
    if (this.routeLine) {
      this.scene.remove(this.routeLine);
      this._disposeObject(this.routeLine);
    }
    if (this.livePathLine) {
      this.scene.remove(this.livePathLine);
      this._disposeObject(this.livePathLine);
      this.livePathLine = null;
    }
    for (const line of this.diagnosticLines) {
      this.scene.remove(line);
      this._disposeObject(line);
    }
    this.diagnosticLines = [];
    for (const marker of this.diagnosticMarkers) {
      this.scene.remove(marker);
      this._disposeObject(marker);
    }
    this.diagnosticMarkers = [];
    this.mesh = null;
    this.wireMesh = null;
    this.grid = null;
    this.marker = null;
    this.liveMarker = null;
    this.liveHeading = null;
    this.routeLine = null;
    this.vertexHeights = null;
    this.meshMinHeight = 0;
    this.meshMaxHeight = 0;
    this.heightFocus = null;
    this.render();
  }

  _disposeObject(object) {
    object.traverse((child) => {
      if (child.geometry) child.geometry.dispose();
      if (child.material) {
        const materials = Array.isArray(child.material) ? child.material : [child.material];
        for (const material of materials) material.dispose();
      }
    });
  }

  _createPathObject(vertices, color = PATH_COLOR, radius = PATH_RADIUS) {
    if (vertices.length < 6) return null;
    const curve = new THREE.CurvePath();
    for (let i = 0; i + 5 < vertices.length; i += 3) {
      const start = new THREE.Vector3(vertices[i], vertices[i + 1], vertices[i + 2]);
      const end = new THREE.Vector3(vertices[i + 3], vertices[i + 4], vertices[i + 5]);
      if (start.distanceToSquared(end) > 1e-8) curve.add(new THREE.LineCurve3(start, end));
    }
    if (curve.curves.length === 0) return null;
    let path;
    if (radius <= 0) {
      const geometry = new THREE.BufferGeometry();
      geometry.setAttribute("position", new THREE.Float32BufferAttribute(vertices, 3));
      path = new THREE.Line(geometry, new THREE.LineBasicMaterial({color, depthTest: false}));
    } else {
      const geometry = new THREE.TubeGeometry(curve, Math.max(1, curve.curves.length), radius, 8, false);
      path = new THREE.Mesh(geometry, new THREE.MeshBasicMaterial({color, depthTest: false}));
    }
    path.renderOrder = 13;
    return path;
  }

  clearSelection() {
    if (!this.marker || !this.marker.visible) return false;
    this.marker.visible = false;
    this.render();
    return true;
  }

  /** @param {"free"|"orbit"} mode */
  setNavigationMode(mode) {
    const nextMode = mode === "orbit" ? "orbit" : "free";
    this._stopMovement();
    this._endFreeLook();
    this.navigationMode = nextMode;
    this.controls.mouseButtons.RIGHT = nextMode === "orbit" ? THREE.MOUSE.ROTATE : null;
  }

  setMovementSpeed(multiplier) {
    this.movementSpeedMultiplier = THREE.MathUtils.clamp(Number(multiplier) || 1, 0.1, 3);
  }

  _heightAt(u, v) {
    if (!this.mesh) return this.meshCenter.height;
    const top = this.meshMaxHeight - this.meshCenter.height;
    const origin = new THREE.Vector3(
      u - this.meshCenter.u,
      Math.max(top + Math.max(1, this.meshRadius * 0.02), this.meshRadius * 0.5),
      v - this.meshCenter.v,
    );
    const ray = new THREE.Raycaster(origin, new THREE.Vector3(0, -1, 0));
    const hits = ray.intersectObject(this.mesh, false);
    const targetHeight = this.heightFocus === null ? this.meshCenter.height : this.heightFocus;
    const hit = hits.reduce((best, candidate) => {
      if (!best) return candidate;
      const bestDistance = Math.abs(best.point.y + this.meshCenter.height - targetHeight);
      const candidateDistance = Math.abs(candidate.point.y + this.meshCenter.height - targetHeight);
      return candidateDistance < bestDistance ? candidate : best;
    }, null);
    return hit ? hit.point.y + this.meshCenter.height : this.meshCenter.height;
  }

  getHeightAt(u, v) {
    return Number.isFinite(u) && Number.isFinite(v) ? this._heightAt(u, v) : null;
  }

  setLivePosition({u, v, height = null, rot = null} = {}) {
    if (!this.liveMarker || !Number.isFinite(u) || !Number.isFinite(v)) return;
    const y = Number.isFinite(height) ? height : this._heightAt(u, v);
    this.liveMarker.position.set(u - this.meshCenter.u, y - this.meshCenter.height, v - this.meshCenter.v);
    this.liveMarker.visible = true;
    if (this.liveHeading) {
      const length = Math.max(this.meshRadius * 0.0045, 0.5);
      const angle = Number.isFinite(rot) ? (rot * Math.PI) / 180 : 0;
      const endX = this.liveMarker.position.x + Math.sin(angle) * length;
      const endZ = this.liveMarker.position.z - Math.cos(angle) * length;
      const attr = this.liveHeading.geometry.getAttribute("position");
      attr.setXYZ(0, this.liveMarker.position.x, this.liveMarker.position.y, this.liveMarker.position.z);
      attr.setXYZ(1, endX, this.liveMarker.position.y, endZ);
      attr.needsUpdate = true;
      this.liveHeading.visible = Number.isFinite(rot);
    }
    this.render();
  }

  setHeightFocus(height) {
    if (!this.mesh || !this.vertexHeights || !Number.isFinite(height)) return;
    const now = performance.now();
    if (
      this.heightFocus !== null &&
      Math.abs(this.heightFocus - height) < 0.5 &&
      now - this.lastHeightColorUpdate < 300
    )
      return;
    this.heightFocus = height;
    this.lastHeightColorUpdate = now;
    const attr = this.mesh.geometry.getAttribute("color");
    const span = 3;
    for (let i = 0; i < this.vertexHeights.length; i += 1) {
      const t = 0.5 + ((this.vertexHeights[i] - height) / span) * 0.5;
      writeHeightColor(attr.array, i * 3, t);
    }
    attr.needsUpdate = true;
  }

  clearLivePosition() {
    if (this.liveMarker) this.liveMarker.visible = false;
    this.render();
  }

  setLivePath(points = []) {
    if (this.livePathLine) {
      this.scene.remove(this.livePathLine);
      this._disposeObject(this.livePathLine);
      this.livePathLine = null;
    }
    if (!this.mesh || !Array.isArray(points) || points.length < 2) {
      this.render();
      return;
    }
    const vertices = [];
    for (const point of points) {
      if (!Array.isArray(point) || point.length < 2) continue;
      const [u, v] = point;
      vertices.push(
        u - this.meshCenter.u,
        this._heightAt(u, v) - this.meshCenter.height + PATH_LIFT,
        v - this.meshCenter.v,
      );
    }
    if (vertices.length < 6) {
      this.render();
      return;
    }
    this.livePathLine = this._createPathObject(vertices, 0x22d3ee);
    if (!this.livePathLine) return;
    this.livePathLine.renderOrder = 14;
    this.scene.add(this.livePathLine);
    this.render();
  }

  setRoute(points = []) {
    if (this.routeLine) {
      this.scene.remove(this.routeLine);
      this._disposeObject(this.routeLine);
      this.routeLine = null;
    }
    if (!this.mesh || !Array.isArray(points) || points.length < 2) return;
    const vertices = [];
    for (const point of points) {
      if (!Array.isArray(point) || point.length < 2) continue;
      const [u, v] = point;
      const height = Number.isFinite(point[2]) ? point[2] : this._heightAt(u, v);
      vertices.push(u - this.meshCenter.u, height - this.meshCenter.height + PATH_LIFT, v - this.meshCenter.v);
    }
    if (vertices.length < 6) return;
    this.routeLine = this._createPathObject(vertices, PATH_COLOR);
    if (!this.routeLine) return;
    this.routeLine.renderOrder = 12;
    this.scene.add(this.routeLine);
    this.render();
  }

  setDiagnostics(diagnostics = [], options = {}) {
    if (this.diagnosticLines) {
      for (const line of this.diagnosticLines) {
        this.scene.remove(line);
        this._disposeObject(line);
      }
    }
    this.diagnosticLines = [];
    for (const marker of this.diagnosticMarkers) {
      this.scene.remove(marker);
      marker.geometry.dispose();
      marker.material.dispose();
    }
    this.diagnosticMarkers = [];
    if (!this.mesh || !Array.isArray(diagnostics)) return;
    const stages = [
      ["astar_cells", "search", 0x38bdf8, 0],
      ["rerouted_points", "rerouted", 0x22c55e, DIAGNOSTIC_RADIUS],
      ["string_pull_points", "stringPull", 0xf59e0b, DIAGNOSTIC_RADIUS],
      ["assembled_points", "assembled", 0xa78bfa, DIAGNOSTIC_RADIUS],
      ["loop_fixed_points", "loopFixed", 0xfb7185, DIAGNOSTIC_RADIUS],
      ["slim_points", "slim", 0x38bdf8, DIAGNOSTIC_RADIUS],
      ["widened_points", "widenCorners", 0xf97316, DIAGNOSTIC_RADIUS],
    ];
    for (const [key, optionKey, color, width] of stages) {
      if (!options[optionKey]) continue;
      for (const diagnostic of diagnostics) {
        const points = diagnostic?.[key];
        if (!Array.isArray(points) || points.length < 1) continue;
        const vertices = [];
        for (const point of points) {
          if (!Array.isArray(point) || point.length < 2) continue;
          const [u, v] = point;
          const height = Number.isFinite(point[2]) ? point[2] : this._heightAt(u, v);
          vertices.push(u - this.meshCenter.u, height - this.meshCenter.height + PATH_LIFT, v - this.meshCenter.v);
        }
        if (vertices.length < 3) continue;
        const markerGeometry = new THREE.BufferGeometry();
        markerGeometry.setAttribute("position", new THREE.Float32BufferAttribute(vertices, 3));
        const marker = new THREE.Points(
          markerGeometry,
          new THREE.PointsMaterial({
            color,
            size:
              key === "astar_cells" ? Math.max(0.12, this.meshRadius * 0.001) : Math.max(0.2, this.meshRadius * 0.0015),
            sizeAttenuation: true,
            depthTest: false,
            transparent: true,
            opacity: 0.95,
          }),
        );
        marker.renderOrder = 15;
        this.scene.add(marker);
        this.diagnosticMarkers.push(marker);
        if (vertices.length >= 6) {
          const line = this._createPathObject(vertices, color, width);
          if (line) {
            line.renderOrder = 13;
            this.scene.add(line);
            this.diagnosticLines.push(line);
          }
        }
      }
    }
    this.render();
  }

  /** @param {boolean} visible */
  setVisible(visible) {
    this.visible = visible;
    this.canvas.hidden = !visible;
    this.controls.enabled = visible;
    if (visible) this.render();
    else {
      this._stopMovement();
      this._endFreeLook();
    }
  }

  /** Toggle the navmesh surface and boundary wire without discarding loaded geometry. @param {boolean} visible */
  setMeshVisible(visible) {
    this.meshLayerVisible = !!visible;
    if (this.mesh) this.mesh.visible = this.meshLayerVisible;
    if (this.wireMesh) this.wireMesh.visible = this.meshLayerVisible;
    if (this.grid) this.grid.visible = this.meshLayerVisible;
    if (!this.meshLayerVisible && this.marker) this.marker.visible = false;
    if (this.visible) this.render();
  }

  /**
   * Update one 3D movement key and start or stop continuous camera movement.
   * @param {string} code
   * @param {boolean} pressed
   * @param {{ctrlKey?:boolean}} modifiers
   * @returns {boolean} whether this view consumed the key transition
   */
  setMovementKey(code, pressed, {ctrlKey = false} = {}) {
    if (code === "Space") {
      if (!pressed) {
        const consumed = this.spacePressed;
        this.spacePressed = false;
        this.movementKeys.delete("SpaceUp");
        this.movementKeys.delete("SpaceDown");
        if (this.movementKeys.size === 0) this._stopMovement();
        return consumed;
      }
      if (!this.visible || !this.mesh || this.navigationMode !== "free") return false;
      this.spacePressed = true;
      if (this._setVerticalMovement(ctrlKey)) this._moveCamera(1 / 60);
      this._startMovement();
      return true;
    }
    if (CONTROL_CODES.has(code)) {
      if (!this.spacePressed || this.navigationMode !== "free") return false;
      if (this._setVerticalMovement(ctrlKey)) this._moveCamera(1 / 60);
      return true;
    }
    if (!MOVEMENT_CODES.has(code)) return false;
    if (!pressed) {
      const consumed = this.movementKeys.delete(code);
      if (this.movementKeys.size === 0) this._stopMovement();
      return consumed;
    }
    if (ctrlKey) return false;
    if (!this.visible || !this.mesh) return false;

    const added = !this.movementKeys.has(code);
    this.movementKeys.add(code);
    if (added) this._moveCamera(1 / 60);
    this._startMovement();
    return true;
  }

  _startMovement() {
    if (!this.movementFrame) {
      this.lastMovementTime = performance.now();
      this.movementFrame = requestAnimationFrame(this._movementFrameBound);
    }
  }

  _setVerticalMovement(descending) {
    const nextKey = descending ? "SpaceDown" : "SpaceUp";
    const changed = !this.movementKeys.has(nextKey);
    this.movementKeys.delete(descending ? "SpaceUp" : "SpaceDown");
    this.movementKeys.add(nextKey);
    return changed;
  }

  /** @param {number} width @param {number} height @param {number} dpr */
  resize(width, height, dpr) {
    this.renderer.setPixelRatio(Math.min(dpr || 1, 2));
    this.renderer.setSize(Math.max(1, width), Math.max(1, height), false);
    this.camera.aspect = Math.max(1, width) / Math.max(1, height);
    this.camera.updateProjectionMatrix();
    this.render();
  }

  fitView() {
    if (!this.mesh) return;
    const verticalFov = THREE.MathUtils.degToRad(this.camera.fov);
    const horizontalFov = 2 * Math.atan(Math.tan(verticalFov / 2) * this.camera.aspect);
    const limitingFov = Math.min(verticalFov, horizontalFov);
    const distance = (this.meshRadius / Math.max(Math.sin(limitingFov / 2), 0.1)) * 0.82;
    const direction = new THREE.Vector3(1, 0.72, 1).normalize();
    this.controls.target.copy(this.focusTarget);
    this.camera.position.copy(direction.multiplyScalar(distance));
    this.camera.near = Math.max(0.05, this.meshRadius / 10000);
    this.camera.far = Math.max(1000, distance + this.meshRadius * 8);
    this.camera.updateProjectionMatrix();
    this.controls.minDistance = this.meshRadius * 0.03;
    this.controls.maxDistance = this.meshRadius * 5;
    this.controls.update();
    this.render();
  }

  /** @param {number} factor values above one zoom in */
  zoomBy(factor) {
    if (!this.mesh) return;
    const offset = this.camera.position.clone().sub(this.controls.target);
    const distance = THREE.MathUtils.clamp(
      offset.length() / factor,
      this.controls.minDistance,
      this.controls.maxDistance,
    );
    offset.setLength(distance);
    this.camera.position.copy(this.controls.target).add(offset);
    this.controls.update();
    this.render();
  }

  render() {
    if (!this.visible) return;
    this.renderer.render(this.scene, this.camera);
  }

  _onMovementFrame(timestamp) {
    this.movementFrame = 0;
    if (!this.visible || !this.mesh || this.movementKeys.size === 0) {
      this.lastMovementTime = null;
      return;
    }

    const deltaSeconds = Math.min(Math.max((timestamp - this.lastMovementTime) / 1000, 0), 0.05);
    this.lastMovementTime = timestamp;
    if (deltaSeconds > 0) this._moveCamera(deltaSeconds);
    this.movementFrame = requestAnimationFrame(this._movementFrameBound);
  }

  _moveCamera(deltaSeconds) {
    const forwardAxis = Number(this.movementKeys.has("KeyW")) - Number(this.movementKeys.has("KeyS"));
    const rightAxis = Number(this.movementKeys.has("KeyD")) - Number(this.movementKeys.has("KeyA"));
    const verticalAxis = Number(this.movementKeys.has("SpaceUp")) - Number(this.movementKeys.has("SpaceDown"));
    if (forwardAxis === 0 && rightAxis === 0 && verticalAxis === 0) return;

    this.camera.getWorldDirection(this.movementForward);
    if (this.navigationMode === "orbit") {
      this.movementForward.y = 0;
      if (this.movementForward.lengthSq() < 1e-8) {
        this.movementRight.setFromMatrixColumn(this.camera.matrixWorld, 0);
        this.movementRight.y = 0;
        this.movementRight.normalize();
        this.movementForward.crossVectors(this.worldUp, this.movementRight);
      } else {
        this.movementForward.normalize();
        this.movementRight.crossVectors(this.movementForward, this.worldUp).normalize();
      }
    } else {
      this.movementForward.normalize();
      this.movementRight.set(1, 0, 0).applyQuaternion(this.camera.quaternion).normalize();
    }

    this.movementDelta
      .copy(this.movementForward)
      .multiplyScalar(forwardAxis)
      .addScaledVector(this.movementRight, rightAxis)
      .addScaledVector(this.worldUp, verticalAxis)
      .normalize();
    const cameraDistance = this.camera.position.distanceTo(this.controls.target);
    const speed =
      THREE.MathUtils.clamp(cameraDistance * 0.4, this.meshRadius * 0.01, this.meshRadius * 1.2) *
      this.movementSpeedMultiplier;
    this.movementDelta.multiplyScalar(speed * deltaSeconds);
    this.camera.position.add(this.movementDelta);
    this.controls.target.add(this.movementDelta);
    this.controls.update();
    this.render();
  }

  _stopMovement() {
    this.movementKeys.clear();
    this.spacePressed = false;
    this.lastMovementTime = null;
    if (this.movementFrame) cancelAnimationFrame(this.movementFrame);
    this.movementFrame = 0;
  }

  _onPointerDown(event) {
    if (!this.visible) return;
    if (event.button === 0) {
      this.pointerDown = {x: event.clientX, y: event.clientY};
    } else if (event.button === 2 && this.navigationMode === "free") {
      this.freeLookPointer = {
        pointerId: event.pointerId,
        x: event.clientX,
        y: event.clientY,
      };
      this.canvas.style.cursor = "grabbing";
    }
  }

  _onPointerMove(event) {
    if (!this.visible || !this.freeLookPointer || event.pointerId !== this.freeLookPointer.pointerId) return;
    if ((event.buttons & 2) === 0) {
      this._endFreeLook(event.pointerId);
      return;
    }

    const deltaX = event.clientX - this.freeLookPointer.x;
    const deltaY = event.clientY - this.freeLookPointer.y;
    this.freeLookPointer.x = event.clientX;
    this.freeLookPointer.y = event.clientY;
    if (deltaX === 0 && deltaY === 0) return;

    const targetDistance = Math.max(this.camera.position.distanceTo(this.controls.target), 1e-6);
    this.lookEuler.setFromQuaternion(this.camera.quaternion, "YXZ");
    this.lookEuler.y -= deltaX * FREE_LOOK_SENSITIVITY;
    this.lookEuler.x = THREE.MathUtils.clamp(
      this.lookEuler.x - deltaY * FREE_LOOK_SENSITIVITY,
      -MAX_FREE_LOOK_PITCH,
      MAX_FREE_LOOK_PITCH,
    );
    this.lookEuler.z = 0;
    this.camera.quaternion.setFromEuler(this.lookEuler);
    this.lookForward.set(0, 0, -1).applyQuaternion(this.camera.quaternion);
    this.controls.target.copy(this.camera.position).addScaledVector(this.lookForward, targetDistance);
    this.controls.update();
    this.render();
  }

  _onPointerUp(event) {
    if (event.button === 2) this._endFreeLook(event.pointerId);
  }

  _endFreeLook(pointerId = null) {
    if (!this.freeLookPointer || (pointerId !== null && pointerId !== this.freeLookPointer.pointerId)) return;
    this.freeLookPointer = null;
    this.canvas.style.cursor = "";
  }

  _onClick(event) {
    if (!this.visible || event.button !== 0 || !this.mesh || !this.meshLayerVisible) return;
    const distance = this.pointerDown
      ? Math.hypot(event.clientX - this.pointerDown.x, event.clientY - this.pointerDown.y)
      : 0;
    this.pointerDown = null;
    if (distance > 4) return;

    const rect = this.canvas.getBoundingClientRect();
    this.pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
    this.pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
    this.raycaster.setFromCamera(this.pointer, this.camera);
    const hit = this.raycaster.intersectObject(this.mesh, false)[0];
    if (!hit) return;

    if (this.marker) {
      this.marker.position.copy(hit.point);
      this.marker.visible = true;
    }
    this.onPick({
      u: hit.point.x + this.meshCenter.u,
      v: hit.point.z + this.meshCenter.v,
      height: hit.point.y + this.meshCenter.height,
    });
    this.render();
  }
}
