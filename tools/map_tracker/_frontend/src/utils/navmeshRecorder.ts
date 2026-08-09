import type { NavEdge, NavMeshDocument, NavVertex, Point } from '@/types'

export interface NavMeshRecorderConfig {
    vertexMergeDistance: number
    vertexMaxDistance: number
    vertexK: number
    edgeBrokenSec: number
    edgeMaxDistance: number
}

export interface NavMeshRecordingResult {
    operation: string
    location: Point | null
    locConf: number | null
    vertexId: number | null
    kValue: number | null
    createdVertex: boolean
    createdEdgeIds: number[]
    skippedEdgeReason: string | null
    dirty: boolean
    chainBroken: boolean
}

interface RecordingPoint {
    vertexId: number | null
    ts: number | null
    mutable: boolean
    originX: number
    originY: number
}

const DEFAULT_CONFIG: NavMeshRecorderConfig = {
    vertexMergeDistance: 4.5,
    vertexMaxDistance: 10.0,
    vertexK: 1.2,
    edgeBrokenSec: 2.0,
    edgeMaxDistance: 15.0,
}

const EDGE_BIDIRECTIONAL = 1

function emptyRecordingPoint(): RecordingPoint {
    return {
        vertexId: null,
        ts: null,
        mutable: false,
        originX: 0,
        originY: 0,
    }
}

function nextId(items: Array<{ id: number }>) {
    return items.reduce((maximum, item) => Math.max(maximum, item.id), 0) + 1
}

function pointToSegmentDistance(
    px: number,
    py: number,
    ax: number,
    ay: number,
    bx: number,
    by: number,
): number {
    const abx = bx - ax
    const aby = by - ay
    const apx = px - ax
    const apy = py - ay
    const ab2 = abx * abx + aby * aby
    if (ab2 < 1e-12) return Math.hypot(apx, apy)
    const t = Math.max(0, Math.min(1, (apx * abx + apy * aby) / ab2))
    return Math.hypot(apx - t * abx, apy - t * aby)
}

/**
 * Ports the legacy Python NavMeshRecorder used by the OpenCV nav_mesh_editor.
 * Mutates the provided NavMeshDocument in place.
 */
export class NavMeshRecorder {
    private readonly config: NavMeshRecorderConfig
    private prevId: number | null = null
    private rp: RecordingPoint = emptyRecordingPoint()
    private mergeZoneVertexId: number | null = null

    constructor(
        private readonly data: NavMeshDocument,
        config?: Partial<NavMeshRecorderConfig>,
    ) {
        this.config = { ...DEFAULT_CONFIG, ...config }
    }

    start() {
        this.prevId = null
        this.rp = emptyRecordingPoint()
        this.mergeZoneVertexId = null
    }

    stop() {
        this.resetChain()
    }

    resetChain() {
        this.prevId = null
        this.rp = emptyRecordingPoint()
        this.mergeZoneVertexId = null
    }

    recordError(): NavMeshRecordingResult {
        this.resetChain()
        return {
            operation: 'error',
            location: null,
            locConf: null,
            vertexId: null,
            kValue: null,
            createdVertex: false,
            createdEdgeIds: [],
            skippedEdgeReason: null,
            dirty: false,
            chainBroken: true,
        }
    }

    acceptLocation(
        x: number,
        y: number,
        locConf: number,
        tierId = 0,
    ): NavMeshRecordingResult {
        const now = Date.now() / 1000
        x = +x.toFixed(3)
        y = +y.toFixed(3)
        const location: Point = [x, y]
        const [vertex, createdVertex, extendedVertex, operation0, kValue] =
            this.generateVertex(x, y, tierId)
        let operation = operation0
        const isMerged = operation === 'merged_vertex'

        if (createdVertex) {
            this.rp.originX = x
            this.rp.originY = y
        }

        const connectFromId = this.rp.vertexId
        const nextMutable = createdVertex || extendedVertex

        let chainBroken = false
        let skippedEdgeReason: string | null = null
        let createdEdgeIds: number[] = []
        if (connectFromId !== null) {
            ;[createdEdgeIds, chainBroken, skippedEdgeReason] = this.connectVertices(
                connectFromId,
                vertex.id,
                now,
            )
            if (createdEdgeIds.length) operation = 'connected'
        }

        if (createdEdgeIds.length) {
            this.prevId = connectFromId
            this.rp.vertexId = vertex.id
            this.rp.mutable = nextMutable
        } else if (chainBroken || this.rp.vertexId === null) {
            this.prevId = null
            this.rp.vertexId = vertex.id
            this.rp.mutable = nextMutable
        } else if (isMerged) {
            this.rp.vertexId = vertex.id
            this.rp.mutable = nextMutable
        } else {
            // "current_vertex": k rejected but within merge distance, keep mutable
            this.rp.mutable = nextMutable || operation === 'current_vertex'
        }

        this.mergeZoneVertexId = isMerged ? vertex.id : null
        this.rp.ts = now

        return {
            operation,
            location,
            locConf,
            vertexId: vertex.id,
            kValue,
            createdVertex,
            createdEdgeIds,
            skippedEdgeReason,
            dirty: createdVertex || extendedVertex || createdEdgeIds.length > 0,
            chainBroken,
        }
    }

    private getVertex(id: number | null | undefined): NavVertex | undefined {
        if (id === null || id === undefined) return undefined
        return this.data.vertices.find((item) => item.id === id)
    }

    private edgesFor(vertexId: number): NavEdge[] {
        return this.data.edges.filter((edge) => edge.from_id === vertexId || edge.to_id === vertexId)
    }

    private neighbors(vertexId: number): NavVertex[] {
        const result: NavVertex[] = []
        for (const edge of this.data.edges) {
            let neighborId: number | null = null
            if (edge.from_id === vertexId) neighborId = edge.to_id
            else if (edge.to_id === vertexId) neighborId = edge.from_id
            if (neighborId === null) continue
            const vertex = this.getVertex(neighborId)
            if (vertex) result.push(vertex)
        }
        return result
    }

    private hasEdgeBetween(aId: number, bId: number) {
        return this.data.edges.some(
            (edge) =>
                (edge.from_id === aId && edge.to_id === bId) ||
                (edge.from_id === bId && edge.to_id === aId),
        )
    }

    private newVertex(x: number, y: number, tierId: number): NavVertex {
        const vertex: NavVertex = {
            id: nextId(this.data.vertices),
            flags: 0,
            x: +x.toFixed(3),
            y: +y.toFixed(3),
            entity_id: 0,
            tier_id: tierId,
        }
        this.data.vertices.push(vertex)
        return vertex
    }

    private newEdge(fromId: number, toId: number): NavEdge {
        const edge: NavEdge = {
            id: nextId(this.data.edges),
            flags: EDGE_BIDIRECTIONAL,
            from_id: fromId,
            to_id: toId,
            cost: 0,
        }
        this.data.edges.push(edge)
        return edge
    }

    private deleteEdge(edgeId: number) {
        this.data.edges = this.data.edges.filter((edge) => edge.id !== edgeId)
    }

    private getNearestVertexFor(
        x: number,
        y: number,
        maxDistance: number,
        tierId: number,
        excludeIds: Set<number>,
    ): NavVertex | undefined {
        let nearest: NavVertex | undefined
        let nearestDist = maxDistance
        for (const vertex of this.data.vertices) {
            if (vertex.tier_id !== tierId) continue
            if (excludeIds.has(vertex.id)) continue
            const dist = Math.hypot(vertex.x - x, vertex.y - y)
            if (dist <= nearestDist) {
                nearest = vertex
                nearestDist = dist
            }
        }
        return nearest
    }

    private static simplifyK(prevP: Point, midP: Point, nextP: Point): number {
        const pmDx = midP[0] - prevP[0]
        const pmDy = midP[1] - prevP[1]
        const mnDx = nextP[0] - midP[0]
        const mnDy = nextP[1] - midP[1]
        const dPrevMid = Math.hypot(pmDx, pmDy)
        const dMidNext = Math.hypot(mnDx, mnDy)
        const sinDtheta = Math.abs(pmDx * mnDy - pmDy * mnDx) / (dPrevMid * dMidNext + 1e-6)
        return Math.min(dMidNext + 1, sinDtheta * (dMidNext + 1))
    }

    private densityDecisionK(current: NavVertex, realtimeP: Point): number | null {
        const values = this.neighbors(current.id).map((neighbor) =>
            NavMeshRecorder.simplifyK([neighbor.x, neighbor.y], [current.x, current.y], realtimeP),
        )
        return values.length ? Math.max(...values) : null
    }

    private nearestMergeVertex(x: number, y: number, tierId: number): NavVertex | undefined {
        const excludeIds = new Set<number>()
        if (this.prevId !== null) excludeIds.add(this.prevId)
        if (this.rp.vertexId !== null) excludeIds.add(this.rp.vertexId)
        return this.getNearestVertexFor(
            x,
            y,
            this.config.vertexMergeDistance,
            tierId,
            excludeIds,
        )
    }

    private createOrExtendVertex(
        x: number,
        y: number,
        tierId: number,
    ): [NavVertex, boolean, boolean, number | null] {
        const current =
            this.rp.vertexId !== null && this.rp.mutable
                ? this.getVertex(this.rp.vertexId)
                : undefined
        if (!current) return [this.newVertex(x, y, tierId), true, false, null]

        // Multi-neighbor vertices are junctions — never extend, always create
        if (this.neighbors(current.id).length >= 2) {
            return [this.newVertex(x, y, tierId), true, false, null]
        }

        // Already extended too far from origin — force create
        if (
            Math.hypot(this.rp.originX - x, this.rp.originY - y) > this.config.vertexMaxDistance
        ) {
            return [this.newVertex(x, y, tierId), true, false, null]
        }

        const decisionK = this.densityDecisionK(current, [x, y])
        if (decisionK !== null && decisionK < this.config.vertexK) {
            current.x = x
            current.y = y
            return [current, false, true, decisionK]
        }

        // k > threshold but still within merge distance: don't create duplicate
        if (Math.hypot(current.x - x, current.y - y) <= this.config.vertexMergeDistance) {
            return [current, false, false, decisionK]
        }

        return [this.newVertex(x, y, tierId), true, false, decisionK]
    }

    private removeOverDistanceEdges(vertex: NavVertex) {
        for (const edge of this.edgesFor(vertex.id)) {
            const neighborId = edge.from_id === vertex.id ? edge.to_id : edge.from_id
            if (neighborId === this.prevId) continue
            const neighbor = this.getVertex(neighborId)
            if (!neighbor) continue
            if (
                Math.hypot(vertex.x - neighbor.x, vertex.y - neighbor.y) >
                this.config.edgeMaxDistance
            ) {
                this.deleteEdge(edge.id)
            }
        }
    }

    private pointInMergeZone(x: number, y: number): boolean {
        const vid = this.mergeZoneVertexId
        if (vid === null) return false
        const vertex = this.getVertex(vid)
        if (!vertex) return false
        const mergeD = this.config.vertexMergeDistance
        for (const edge of this.edgesFor(vid)) {
            const otherId = edge.from_id === vid ? edge.to_id : edge.from_id
            const other = this.getVertex(otherId)
            if (!other) continue
            if (
                pointToSegmentDistance(x, y, vertex.x, vertex.y, other.x, other.y) <= mergeD
            ) {
                return true
            }
        }
        return Math.hypot(x - vertex.x, y - vertex.y) <= mergeD
    }

    private generateVertex(
        x: number,
        y: number,
        tierId: number,
    ): [NavVertex, boolean, boolean, string, number | null] {
        // Always try to find a merge target first
        const mergeTarget = this.nearestMergeVertex(x, y, tierId)
        if (mergeTarget) return [mergeTarget, false, false, 'merged_vertex', null]

        // No merge target found — fall back to merge zone corridor
        if (this.pointInMergeZone(x, y)) {
            const zoneVertex = this.getVertex(this.mergeZoneVertexId)
            if (zoneVertex && zoneVertex.tier_id === tierId) {
                return [zoneVertex, false, false, 'merged_vertex', null]
            }
        }

        const oldX = this.rp.originX
        const oldY = this.rp.originY
        const [vertex, createdVertex, extendedVertex, kValue] = this.createOrExtendVertex(
            x,
            y,
            tierId,
        )
        if (createdVertex) return [vertex, true, false, 'created_vertex', kValue]
        if (extendedVertex) {
            // Clean up edges that became over-distance after extension
            if (Math.hypot(oldX - x, oldY - y) > 0) this.removeOverDistanceEdges(vertex)
            return [vertex, false, true, 'extended_vertex', kValue]
        }
        return [vertex, false, false, 'current_vertex', kValue]
    }

    private connectVertices(
        fromId: number,
        toId: number,
        now: number,
    ): [number[], boolean, string | null] {
        if (fromId === toId) return [[], false, null]
        if (this.rp.ts !== null && now - this.rp.ts > this.config.edgeBrokenSec) {
            return [[], true, 'time_gap']
        }
        const src = this.getVertex(fromId)
        const dst = this.getVertex(toId)
        if (!src || !dst) {
            this.resetChain()
            return [[], true, 'missing_vertex']
        }
        if (Math.hypot(src.x - dst.x, src.y - dst.y) > this.config.edgeMaxDistance) {
            return [[], true, 'distance']
        }
        if (this.hasEdgeBetween(fromId, toId)) return [[], false, null]
        const edge = this.newEdge(fromId, toId)
        return [[edge.id], false, null]
    }
}
