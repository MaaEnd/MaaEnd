import type { MapInfo, MapLayerInfo } from '@/types'

export function stripMapExtension(mapName: string) {
    return mapName.replace(/\.[^.]+$/, '')
}

export function mapTierOptions(map: MapInfo | undefined, baseTierLabel: string) {
    return (
        map?.layers.map((layer) => ({
            label: layer.id === 'main' ? baseTierLabel : layer.label,
            value: layer.file_name,
        })) ?? []
    )
}

export function mapNameKey(mapName: string) {
    return stripMapExtension(mapName).toLowerCase()
}

export function findMapByName(maps: MapInfo[], mapName: string): MapInfo | undefined {
    const key = mapNameKey(mapName)
    if (!key) return undefined
    return maps.find((map) => map.layers.some((layer) => mapNameKey(layer.map_name) === key))
}

export function findLayerByName(map: MapInfo | undefined, mapName: string): MapLayerInfo | undefined {
    const key = mapNameKey(mapName)
    if (!key || !map) return undefined
    return map.layers.find((layer) => mapNameKey(layer.map_name) === key)
}

export function tierIdOf(mapName: string) {
    return Number(mapName.match(/_tier_(\d+)/)?.[1] ?? 0)
}

export function isInteractiveTarget(target: EventTarget | null) {
    return (
        target instanceof HTMLElement &&
        (target.isContentEditable || ['BUTTON', 'INPUT', 'SELECT', 'TEXTAREA'].includes(target.tagName))
    )
}

export function readLocalFlag(key: string, fallback = false) {
    try {
        const value = localStorage.getItem(key)
        if (value === null) return fallback
        return value === '1' || value === 'true'
    } catch {
        return fallback
    }
}

export function writeLocalFlag(key: string, value: boolean) {
    try {
        localStorage.setItem(key, value ? '1' : '0')
    } catch {
        // Persistence may be unavailable.
    }
}

export function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error)
}
