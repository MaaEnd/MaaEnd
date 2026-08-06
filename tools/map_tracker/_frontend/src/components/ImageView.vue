<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(
    defineProps<{
        src: string
        alt?: string
        minZoom?: number
        maxZoom?: number
    }>(),
    {
        alt: '',
        minZoom: 0.1,
        maxZoom: 8,
    },
)

const host = ref<HTMLElement | null>(null)
const image = ref<HTMLImageElement | null>(null)
const scale = ref(1)
const offsetX = ref(0)
const offsetY = ref(0)
const panning = ref(false)
const guideLine = ref<{ x1: number; y1: number; x2: number; y2: number } | null>(null)
let panOrigin = { pointerX: 0, pointerY: 0, offsetX: 0, offsetY: 0 }
let hasViewport = false
let resizeObserver: ResizeObserver | undefined

const guideMetrics = computed(() => {
    const line = guideLine.value
    if (!line) return null
    const dx = line.x2 - line.x1
    const dy = line.y2 - line.y1
    const screenLength = Math.hypot(dx, dy)
    if (screenLength < 0.5) return null
    // Sample heading: 0° is up, clockwise increases.
    const degrees = ((Math.atan2(dx, -dy) * 180) / Math.PI + 360) % 360
    return {
        label: `${degrees.toFixed(1)}°, ${(screenLength / scale.value).toFixed(1)}px`,
        x: line.x2 + 8,
        y: line.y2 - 8,
    }
})

function clampZoom(value: number): number {
    return Math.min(props.maxZoom, Math.max(props.minZoom, value))
}

function pointerInHost(event: PointerEvent): { x: number; y: number } | null {
    const hostEl = host.value
    if (!hostEl) return null
    const rect = hostEl.getBoundingClientRect()
    return { x: event.clientX - rect.left, y: event.clientY - rect.top }
}

function fit() {
    const hostEl = host.value
    const imageEl = image.value
    if (!hostEl || !imageEl || !imageEl.naturalWidth || !imageEl.naturalHeight) return
    const rect = hostEl.getBoundingClientRect()
    if (rect.width <= 0 || rect.height <= 0) return
    const nextScale = clampZoom(
        Math.min(rect.width / imageEl.naturalWidth, rect.height / imageEl.naturalHeight),
    )
    scale.value = nextScale
    offsetX.value = (rect.width - imageEl.naturalWidth * nextScale) / 2
    offsetY.value = (rect.height - imageEl.naturalHeight * nextScale) / 2
    hasViewport = true
}

function onImageLoad() {
    if (!hasViewport) fit()
}

function onWheel(event: WheelEvent) {
    const hostEl = host.value
    if (!hostEl) return
    event.preventDefault()
    const rect = hostEl.getBoundingClientRect()
    const pointerX = event.clientX - rect.left
    const pointerY = event.clientY - rect.top
    const nextScale = clampZoom(scale.value * (event.deltaY < 0 ? 1.1 : 1 / 1.1))
    if (nextScale === scale.value) return
    const worldX = (pointerX - offsetX.value) / scale.value
    const worldY = (pointerY - offsetY.value) / scale.value
    scale.value = nextScale
    offsetX.value = pointerX - worldX * nextScale
    offsetY.value = pointerY - worldY * nextScale
    hasViewport = true
}

function bindPointerSession(
    target: HTMLElement,
    pointerId: number,
    onMove: (event: PointerEvent) => void,
    onEnd: () => void,
) {
    target.setPointerCapture(pointerId)
    const handleUp = (event: PointerEvent) => {
        target.releasePointerCapture(event.pointerId)
        target.removeEventListener('pointermove', onMove)
        target.removeEventListener('pointerup', handleUp)
        target.removeEventListener('pointercancel', handleUp)
        onEnd()
    }
    target.addEventListener('pointermove', onMove)
    target.addEventListener('pointerup', handleUp)
    target.addEventListener('pointercancel', handleUp)
}

function startPan(event: PointerEvent) {
    panning.value = true
    panOrigin = {
        pointerX: event.clientX,
        pointerY: event.clientY,
        offsetX: offsetX.value,
        offsetY: offsetY.value,
    }
    bindPointerSession(
        event.currentTarget as HTMLElement,
        event.pointerId,
        (moveEvent) => {
            offsetX.value = panOrigin.offsetX + (moveEvent.clientX - panOrigin.pointerX)
            offsetY.value = panOrigin.offsetY + (moveEvent.clientY - panOrigin.pointerY)
            hasViewport = true
        },
        () => {
            panning.value = false
        },
    )
}

function startGuideLine(event: PointerEvent) {
    const point = pointerInHost(event)
    if (!point) return
    guideLine.value = { x1: point.x, y1: point.y, x2: point.x, y2: point.y }
    bindPointerSession(
        event.currentTarget as HTMLElement,
        event.pointerId,
        (moveEvent) => {
            const next = pointerInHost(moveEvent)
            const line = guideLine.value
            if (!next || !line) return
            line.x2 = next.x
            line.y2 = next.y
        },
        () => {
            guideLine.value = null
        },
    )
}

function onPointerDown(event: PointerEvent) {
    if (event.button === 1 || event.button === 2) {
        event.preventDefault()
        startPan(event)
        return
    }
    if (event.button !== 0) return
    event.preventDefault()
    startGuideLine(event)
}

onMounted(() => {
    if (!host.value) return
    resizeObserver = new ResizeObserver(() => {
        if (!hasViewport) fit()
    })
    resizeObserver.observe(host.value)
})

onBeforeUnmount(() => resizeObserver?.disconnect())
</script>

<template>
    <div
        ref="host"
        class="relative h-full min-h-0 overflow-hidden bg-black/25"
        :class="panning ? 'cursor-grabbing' : 'cursor-crosshair'"
        @wheel="onWheel"
        @pointerdown="onPointerDown"
        @contextmenu.prevent
        @dblclick="fit">
        <img
            ref="image"
            :src="src"
            :alt="alt"
            draggable="false"
            class="absolute top-0 left-0 max-w-none select-none"
            :style="{
                transform: `translate(${offsetX}px, ${offsetY}px) scale(${scale})`,
                transformOrigin: '0 0',
            }"
            @load="onImageLoad"
            @dragstart.prevent />
        <svg
            v-if="guideLine"
            class="pointer-events-none absolute inset-0 size-full"
            aria-hidden="true">
            <line
                :x1="guideLine.x1"
                :y1="guideLine.y1"
                :x2="guideLine.x2"
                :y2="guideLine.y2"
                stroke="#f56c6c"
                stroke-width="2"
                stroke-linecap="round" />
            <text
                v-if="guideMetrics"
                :x="guideMetrics.x"
                :y="guideMetrics.y"
                fill="#ffffff"
                stroke="#000000"
                stroke-width="3"
                paint-order="stroke"
                font-size="12"
                font-family="ui-monospace, SFMono-Regular, Menlo, Consolas, monospace">
                {{ guideMetrics.label }}
            </text>
        </svg>
    </div>
</template>
