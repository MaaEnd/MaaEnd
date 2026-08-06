import type {
    MapInfo,
    NavMeshDocument,
    PipelineFile,
    SampleCollectionStatus,
    SampleItem,
    SampleMetadata,
    StaticInferResult,
    TrackPeriod,
} from './types'

async function request<T>(url: string, init?: RequestInit): Promise<T> {
    const response = await fetch(url, init)
    if (!response.ok) {
        let message = `${response.status} ${response.statusText}`
        try {
            const body = (await response.json()) as { detail?: string }
            message = body.detail || message
        } catch {
            // Keeps the HTTP status when the response is not JSON.
        }
        throw new Error(message)
    }
    return response.json() as Promise<T>
}

const jsonInit = (method: string, body: unknown): RequestInit => ({
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
})

export const api = {
    async maps(): Promise<MapInfo[]> {
        return (await request<{ maps: MapInfo[] }>('/api/maps')).maps
    },
    mapImage(fileName: string): string {
        const composite = fileName.includes('_tier_') ? '?composite=true' : ''
        return `/api/maps/image/${encodeURIComponent(fileName)}${composite}`
    },
    async pipelineFiles(): Promise<PipelineFile[]> {
        return (await request<{ files: PipelineFile[] }>('/api/pipeline/maptracker-nodes')).files
    },
    savePipelineNode(body: unknown): Promise<{ revision: string }> {
        return request('/api/pipeline/node', jsonInit('PUT', body))
    },
    analyseLog(text: string): Promise<{
        point_count: number
        warnings: Array<{ line: number; message: string }>
        periods: TrackPeriod[]
    }> {
        return request('/api/log/analyse', {
            method: 'POST',
            headers: { 'Content-Type': 'text/plain;charset=UTF-8' },
            body: text,
        })
    },
    async navmeshes(): Promise<string[]> {
        return (await request<{ files: string[] }>('/api/navmeshes')).files
    },
    loadNavmesh(name: string): Promise<NavMeshDocument> {
        return request(`/api/navmeshes/${name.split('/').map(encodeURIComponent).join('/')}`)
    },
    newNavmesh(mapFile: string): Promise<NavMeshDocument> {
        return request('/api/navmeshes/new', jsonInit('POST', { map_file: mapFile }))
    },
    saveNavmesh(document: NavMeshDocument): Promise<{ revision: string }> {
        const name = document.name.split('/').map(encodeURIComponent).join('/')
        return request(`/api/navmeshes/${name}`, jsonInit('PUT', document))
    },
    infer(mapName: string): Promise<{
        map_name: string
        x: number
        y: number
        loc_conf: number
        rot: number
        rot_conf: number
    }> {
        return request('/api/location/infer', jsonInit('POST', { map_name: mapName }))
    },
    async inferImage(file: File, precision: number): Promise<StaticInferResult> {
        const query = new URLSearchParams({ precision: precision.toFixed(1) })
        return request(`/api/location/infer-image?${query}`, {
            method: 'POST',
            body: new Uint8Array(await file.arrayBuffer()),
            headers: { 'Content-Type': 'application/octet-stream' },
        })
    },
    goal(mapName: string, x: number, y: number): Promise<{ ok: boolean }> {
        return request('/api/location/goal', jsonInit('POST', { map_name: mapName, x, y }))
    },
    sampleCollectionStatus(): Promise<SampleCollectionStatus> {
        return request('/api/samples/status')
    },
    loadSampleDirectory(outputDir: string): Promise<SampleCollectionStatus> {
        return request('/api/samples/load', jsonInit('POST', { output_dir: outputDir }))
    },
    startSampleCollection(outputDir: string): Promise<SampleCollectionStatus> {
        return request('/api/samples/start', jsonInit('POST', { output_dir: outputDir }))
    },
    stopSampleCollection(): Promise<SampleCollectionStatus> {
        return request('/api/samples/stop', { method: 'POST' })
    },
    sampleImage(sampleId: string): string {
        return `/api/samples/${encodeURIComponent(sampleId)}/image`
    },
    saveSample(
        sampleId: string,
        metadata: SampleMetadata,
    ): Promise<{ saved: SampleItem; status: SampleCollectionStatus }> {
        return request(
            `/api/samples/${encodeURIComponent(sampleId)}/save`,
            jsonInit('POST', metadata),
        )
    },
    updateSample(
        sampleId: string,
        metadata: SampleMetadata,
    ): Promise<{ updated: SampleItem; status: SampleCollectionStatus }> {
        return request(`/api/samples/${encodeURIComponent(sampleId)}`, jsonInit('PUT', metadata))
    },
}
