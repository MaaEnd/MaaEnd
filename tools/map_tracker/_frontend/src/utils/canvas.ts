export interface Viewport {
    x: number
    y: number
    zoom: number
    width: number
    height: number
}

export interface MapLayer {
    id: string
    zIndex: number
    visible?: boolean
    draw: (context: CanvasRenderingContext2D, viewport: Viewport) => void
}

export interface MapPointerInfo {
    x: number
    y: number
    screenX: number
    screenY: number
    button: number
    buttons: number
    viewport: Viewport
}

export type Point2 = [number, number]

export interface FitToOptions {
    padding?: number
    minZoom?: number
    maxZoom?: number
}

export function mapToScreen(viewport: Viewport, x: number, y: number): [number, number] {
    return [(x - viewport.x) * viewport.zoom, (y - viewport.y) * viewport.zoom]
}

/** Fits the viewport to a point set (Python ViewportManager.fit_to). */
export function fitViewportTo(
    viewport: Viewport,
    points: Point2[],
    limits: { minZoom: number; maxZoom: number },
    options: FitToOptions = {},
): Viewport {
    if (!points.length) return { ...viewport }
    let minX = points[0][0]
    let maxX = points[0][0]
    let minY = points[0][1]
    let maxY = points[0][1]
    for (const [x, y] of points) {
        if (x < minX) minX = x
        if (x > maxX) maxX = x
        if (y < minY) minY = y
        if (y > maxY) maxY = y
    }
    const spanX = Math.max(1, maxX - minX)
    const spanY = Math.max(1, maxY - minY)
    const padding = Math.max(0, Math.min(0.49, options.padding ?? 0))
    const fitW = Math.max(1, viewport.width * (1 - 2 * padding))
    const fitH = Math.max(1, viewport.height * (1 - 2 * padding))
    const targetZoom = Math.min(fitW / spanX, fitH / spanY)
    const minZoom = options.minZoom === undefined ? limits.minZoom : Math.max(limits.minZoom, options.minZoom)
    const maxZoom = options.maxZoom === undefined ? limits.maxZoom : Math.min(limits.maxZoom, options.maxZoom)
    const zoom = Math.max(minZoom, Math.min(maxZoom, targetZoom))
    const viewW = viewport.width / zoom
    const viewH = viewport.height / zoom
    return {
        ...viewport,
        zoom,
        x: (minX + maxX) / 2 - viewW / 2,
        y: (minY + maxY) / 2 - viewH / 2,
    }
}

/** Soft-follows a point only when it leaves the padded safe region. */
export function maybeCenterViewport(
    viewport: Viewport,
    x: number,
    y: number,
    padding = 0.3,
): Viewport {
    const pad = Math.max(0, Math.min(0.49, padding))
    const viewW = viewport.width / viewport.zoom
    const viewH = viewport.height / viewport.zoom
    const padW = viewW * pad
    const padH = viewH * pad
    const left = viewport.x + padW
    const right = viewport.x + viewW - padW
    const top = viewport.y + padH
    const bottom = viewport.y + viewH - padH
    if (x >= left && x <= right && y >= top && y <= bottom) return { ...viewport }
    return {
        ...viewport,
        x: x - viewW / 2,
        y: y - viewH / 2,
    }
}

export function distanceToSegment(
    px: number,
    py: number,
    ax: number,
    ay: number,
    bx: number,
    by: number,
): number {
    const dx = bx - ax
    const dy = by - ay
    if (!dx && !dy) return Math.hypot(px - ax, py - ay)
    const t = Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)))
    return Math.hypot(px - ax - t * dx, py - ay - t * dy)
}

/** Draws a full-viewport crosshair through a map coordinate (used while recording). */
export function drawMapCrosshair(
    context: CanvasRenderingContext2D,
    viewport: Viewport,
    x: number,
    y: number,
    color = '#67c23a',
) {
    const [screenX, screenY] = mapToScreen(viewport, x, y)
    context.save()
    context.lineCap = 'butt'
    context.strokeStyle = color
    context.lineWidth = 1.5
    context.globalAlpha = 0.9
    context.beginPath()
    context.moveTo(0, screenY)
    context.lineTo(viewport.width, screenY)
    context.moveTo(screenX, 0)
    context.lineTo(screenX, viewport.height)
    context.stroke()
    context.globalAlpha = 1
    context.fillStyle = color
    context.strokeStyle = '#ffffff'
    context.lineWidth = 1.5
    context.beginPath()
    context.arc(screenX, screenY, 4, 0, Math.PI * 2)
    context.fill()
    context.stroke()
    context.restore()
}

/** Draws a heading arrow and position dot at a map coordinate. */
export function drawLocationPose(
    context: CanvasRenderingContext2D,
    viewport: Viewport,
    x: number,
    y: number,
    rot: number,
    color = '#f56c6c',
) {
    const [screenX, screenY] = mapToScreen(viewport, x, y)
    context.save()
    context.translate(screenX, screenY)
    context.rotate((rot * Math.PI) / 180)
    context.strokeStyle = color
    context.lineCap = 'round'
    context.lineJoin = 'round'
    context.lineWidth = 3
    context.beginPath()
    context.moveTo(0, 7)
    context.lineTo(0, -30)
    context.moveTo(-7, -22)
    context.lineTo(0, -30)
    context.lineTo(7, -22)
    context.stroke()
    context.restore()

    context.save()
    context.fillStyle = color
    context.strokeStyle = '#ffffff'
    context.lineWidth = 2
    context.beginPath()
    context.arc(screenX, screenY, 6, 0, Math.PI * 2)
    context.fill()
    context.stroke()
    context.restore()
}
