export type Point = [number, number]
export type Rect = [number, number, number, number]
export type NodeType = 'MapTrackerMove' | 'MapTrackerAssertLocation' | 'MapTrackerGoal'

export interface MapLayerInfo {
    id: string
    label: string
    file_name: string
    map_name: string
}

export interface MapInfo {
    id: string
    name: string
    width: number
    height: number
    layers: MapLayerInfo[]
}

export interface EditableNode {
    id: string
    name: string
    node_type: NodeType
    map_name: string
    path: Point[]
    target: Rect | null
    is_new_structure?: boolean
    created_at: string
    updated_at: string
    source?: { file_path: string; revision: string }
}

export interface PipelineFile {
    path: string
    name: string
    revision: string
    nodes: Array<{
        node_name: string
        node_type: NodeType
        map_name: string
        path: Point[]
        target: Rect | null
        is_new_structure: boolean
    }>
}

export interface TrackPoint {
    map_name: string
    x: number
    y: number
    rot: number
    timestamp: number | null
    loc_conf: number
    rot_conf: number
}

export interface TrackPeriod {
    period_id: number
    points: TrackPoint[]
    start_timestamp: number | null
    end_timestamp: number | null
}

export interface NavVertex {
    id: number
    flags: number
    x: number
    y: number
    entity_id: number
    tier_id: number
}

export interface NavEdge {
    id: number
    flags: number
    from_id: number
    to_id: number
    cost: number
}

export interface NavMeshDocument {
    name: string
    map_file: string
    revision: string | null
    meta: {
        name: string
        description: string
        map_region_name: string
        map_level_name: string
        geo_width: number
        geo_height: number
    }
    vertices: NavVertex[]
    edges: NavEdge[]
    imported_entities?: number
}

export type SampleKind = 'candidate' | 'existing'

export interface SampleMetadata {
    map_name: string
    x: number
    y: number
    rot: number
}

export interface SampleInference {
    infer_mode: string
    infer_time_ms: number
    loc_conf: number
    loc_time_ms: number
    rot_conf: number
    rot_time_ms: number
}

export interface SampleItem extends SampleMetadata {
    id: string
    kind: SampleKind
    filename: string
    timestamp: number
    inference: SampleInference | null
}

export interface SampleCollectionStatus {
    running: boolean
    output_dir: string
    candidate_limit: number
    error: string | null
    candidates: SampleItem[]
    existing: SampleItem[]
}

export interface StaticInferResult {
    infer_mode: string
    infer_time_ms: number
    loc_conf: number
    loc_time_ms: number
    map_name: string
    precision: number
    rot: number
    rot_conf: number
    rot_time_ms: number
    x: number
    y: number
}

export interface StaticInferHistoryItem {
    id: string
    filename: string
    file: File
    imageUrl: string
    precision: number
    result: StaticInferResult | null
    error: string | null
    createdAt: number
}
