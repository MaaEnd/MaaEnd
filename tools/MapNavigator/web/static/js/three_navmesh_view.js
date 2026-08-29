import * as THREE from "three";
import {OrbitControls} from "three/addons/controls/OrbitControls.js";

import {buildBoundaryEdgeIndices, parseNmsh} from "./navmesh_3d_data.js";

const LOW_COLOR = [0.01, 0.06, 0.18];
const MID_COLOR = [0.0, 0.36, 0.26];
const HIGH_COLOR = [0.85, 0.28, 0.02];
const MOVEMENT_CODES = new Set(["KeyW", "KeyA", "KeyS", "KeyD"]);

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
        this.controls.addEventListener("change", () => this.render());

        this.mesh = null;
        this.wireMesh = null;
        this.grid = null;
        this.marker = null;
        this.meshCenter = {u: 0, v: 0, height: 0};
        this.focusTarget = new THREE.Vector3();
        this.meshRadius = 1;
        this.visible = false;
        this.pointerDown = null;
        this.raycaster = new THREE.Raycaster();
        this.pointer = new THREE.Vector2();
        this.movementKeys = new Set();
        this.movementFrame = 0;
        this.lastMovementTime = null;
        this.movementForward = new THREE.Vector3();
        this.movementRight = new THREE.Vector3();
        this.movementDelta = new THREE.Vector3();
        this.worldUp = new THREE.Vector3(0, 1, 0);
        this._movementFrameBound = (timestamp) => this._onMovementFrame(timestamp);

        canvas.addEventListener("pointerdown", (event) => this._onPointerDown(event));
        canvas.addEventListener("click", (event) => this._onClick(event));
        canvas.addEventListener("pointercancel", () => {
            this.pointerDown = null;
        });
        canvas.addEventListener("contextmenu", (event) => event.preventDefault());
        window.addEventListener("blur", () => this._stopMovement());
    }

    /** @param {ArrayBuffer} buffer */
    setMesh(buffer) {
        const parsed = parseNmsh(buffer);
        this.clearMesh();

        const {minU, maxU, minV, maxV, minHeight, maxHeight} = parsed.bounds;
        this.meshCenter = {
            u: (minU + maxU) / 2,
            v: (minV + maxV) / 2,
            height: (minHeight + maxHeight) / 2,
        };

        const positions = new Float32Array(parsed.vertices.length);
        const colors = new Float32Array(parsed.vertices.length);
        const heightSpan = Math.max(1e-6, maxHeight - minHeight);
        for (let i = 0; i < parsed.vertexCount; i += 1) {
            const offset = i * 3;
            positions[offset] = parsed.vertices[offset] - this.meshCenter.u;
            positions[offset + 1] = parsed.vertices[offset + 2] - this.meshCenter.height;
            positions[offset + 2] = parsed.vertices[offset + 1] - this.meshCenter.v;
            writeHeightColor(colors, offset, (parsed.vertices[offset + 2] - minHeight) / heightSpan);
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
        wireGeometry.setIndex(
            new THREE.BufferAttribute(buildBoundaryEdgeIndices(parsed.indices, parsed.vertexCount), 1),
        );
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

        const markerGeometry = new THREE.SphereGeometry(Math.max(0.6, this.meshRadius * 0.004), 20, 12);
        const markerMaterial = new THREE.MeshBasicMaterial({color: 0xffbd4a, depthTest: false});
        this.marker = new THREE.Mesh(markerGeometry, markerMaterial);
        this.marker.visible = false;
        this.marker.renderOrder = 10;
        this.scene.add(this.marker);

        this.fitView();
    }

    clearMesh() {
        this._stopMovement();
        if (this.mesh) {
            this.scene.remove(this.mesh);
            this.mesh.geometry.dispose();
            this.mesh.material.dispose();
        }
        if (this.wireMesh) {
            this.scene.remove(this.wireMesh);
            this.wireMesh.geometry.dispose();
            this.wireMesh.material.dispose();
        }
        if (this.grid) {
            this.scene.remove(this.grid);
            this.grid.geometry.dispose();
            this.grid.material.dispose();
        }
        if (this.marker) {
            this.scene.remove(this.marker);
            this.marker.geometry.dispose();
            this.marker.material.dispose();
        }
        this.mesh = null;
        this.wireMesh = null;
        this.grid = null;
        this.marker = null;
        this.render();
    }

    clearSelection() {
        if (!this.marker || !this.marker.visible) return false;
        this.marker.visible = false;
        this.render();
        return true;
    }

    /** @param {boolean} visible */
    setVisible(visible) {
        this.visible = visible;
        this.canvas.hidden = !visible;
        this.controls.enabled = visible;
        if (visible) this.render();
        else this._stopMovement();
    }

    /**
     * Update one WASD key and start or stop continuous camera movement.
     * @param {string} code
     * @param {boolean} pressed
     * @returns {boolean} whether this view consumed the key transition
     */
    setMovementKey(code, pressed) {
        if (!MOVEMENT_CODES.has(code)) return false;
        if (!pressed) {
            const consumed = this.movementKeys.delete(code);
            if (this.movementKeys.size === 0) this._stopMovement();
            return consumed;
        }
        if (!this.visible || !this.mesh) return false;

        const added = !this.movementKeys.has(code);
        this.movementKeys.add(code);
        if (added) this._moveCamera(1 / 60);
        if (!this.movementFrame) {
            this.lastMovementTime = performance.now();
            this.movementFrame = requestAnimationFrame(this._movementFrameBound);
        }
        return true;
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
        const distance = (this.meshRadius / Math.max(Math.sin(limitingFov / 2), 0.1)) * 1.12;
        const direction = new THREE.Vector3(1, 0.72, 1).normalize();
        this.controls.target.copy(this.focusTarget);
        this.camera.position.copy(direction.multiplyScalar(distance));
        this.camera.near = Math.max(0.05, this.meshRadius / 10000);
        this.camera.far = Math.max(1000, distance + this.meshRadius * 8);
        this.camera.updateProjectionMatrix();
        this.controls.minDistance = this.meshRadius * 0.03;
        this.controls.maxDistance = this.meshRadius * 10;
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
        if (forwardAxis === 0 && rightAxis === 0) return;

        this.camera.getWorldDirection(this.movementForward);
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

        this.movementDelta
            .copy(this.movementForward)
            .multiplyScalar(forwardAxis)
            .addScaledVector(this.movementRight, rightAxis)
            .normalize();
        const cameraDistance = this.camera.position.distanceTo(this.controls.target);
        const speed = THREE.MathUtils.clamp(cameraDistance * 0.4, this.meshRadius * 0.08, this.meshRadius * 1.2);
        this.movementDelta.multiplyScalar(speed * deltaSeconds);
        this.camera.position.add(this.movementDelta);
        this.controls.target.add(this.movementDelta);
        this.controls.update();
        this.render();
    }

    _stopMovement() {
        this.movementKeys.clear();
        this.lastMovementTime = null;
        if (this.movementFrame) cancelAnimationFrame(this.movementFrame);
        this.movementFrame = 0;
    }

    _onPointerDown(event) {
        if (!this.visible || event.button !== 0) return;
        this.pointerDown = {x: event.clientX, y: event.clientY};
    }

    _onClick(event) {
        if (!this.visible || event.button !== 0 || !this.mesh) return;
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
