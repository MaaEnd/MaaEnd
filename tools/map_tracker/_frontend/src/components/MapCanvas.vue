<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { FullScreen, Loading, Minus, Plus } from '@element-plus/icons-vue'
import {
    fitViewportTo,
    maybeCenterViewport,
    type FitToOptions,
    type MapLayer,
    type MapPointerInfo,
    type Point2,
    type Viewport,
} from '@/utils/canvas'

const { t } = useI18n()

const TRANSITION_MS = 200

const props = withDefaults(
    defineProps<{
        imageUrl?: string
        imageWidth?: number
        imageHeight?: number
        viewportKey?: string
        layers?: MapLayer[]
        minZoom?: number
        maxZoom?: number
        tierOptions?: Array<{ label: string; value: string }>
        tierValue?: string
        tierDisabled?: boolean
        focusPoint?: [number, number]
        focusWidth?: number
        focusHeight?: number
    }>(),
    {
        imageUrl: '',
        imageWidth: 1280,
        imageHeight: 720,
        viewportKey: '',
        layers: () => [],
        minZoom: 0.25,
        maxZoom: 16,
        tierOptions: () => [],
        tierValue: '',
        tierDisabled: false,
        focusWidth: 480,
        focusHeight: 360,
    },
)

const emit = defineEmits<{
    pointerdown: [event: MapPointerInfo]
    pointermove: [event: MapPointerInfo]
    pointerup: [event: MapPointerInfo]
    viewport: [viewport: Viewport]
    ready: []
    'update:tierValue': [value: string]
}>()

const host = ref<HTMLDivElement>()
const canvas = ref<HTMLCanvasElement>()
let displayImage = new Image()
let displayWidth = 1280
let displayHeight = 720
const imageReady = ref(false)
const imageLoading = ref(false)
const imageError = ref(false)
const fadeIn = ref(1)
const viewport = ref<Viewport>({ x: 0, y: 0, zoom: 1, width: 1, height: 1 })
const pointerLocation = ref<[number, number]>([
    0,
    0,
])
const pointerScreen = ref<[number, number]>([
    0,
    0,
])
const pointerVisible = ref(false)
const dragging = ref(false)
const panning = ref(false)
let fittedKey = ''
/** Last map key that received a viewport target; used to skip pan/zoom tween across maps. */
let viewportAnimKey = ''
let loadToken = 0
let panStart: [number, number, number, number] = [
    0,
    0,
    0,
    0,
]
let resizeObserver: ResizeObserver | undefined
let viewportAnimId = 0
let fadeAnimId = 0
const previousSnapshot = shallowRef<{
    image: HTMLImageElement
    width: number
    height: number
    viewport: Viewport
} | null>(null)

const sortedLayers = computed(() => [...props.layers].sort((a, b) => a.zIndex - b.zIndex))
const tierIndex = computed(() => props.tierOptions.findIndex((option) => option.value === props.tierValue))
const currentTier = computed(() => props.tierOptions[tierIndex.value])
const cursorClass = computed(() => (dragging.value ? 'cursor-grabbing' : 'cursor-crosshair'))
const showLoading = computed(() => imageLoading.value && !previousSnapshot.value && !imageReady.value)

function easeOutCubic(t: number) {
    return 1 - (1 - t) ** 3
}

function cancelViewportAnim() {
    if (!viewportAnimId) return
    cancelAnimationFrame(viewportAnimId)
    viewportAnimId = 0
}

function cancelFadeAnim() {
    if (!fadeAnimId) return
    cancelAnimationFrame(fadeAnimId)
    fadeAnimId = 0
}

function mapViewportKey() {
    return props.viewportKey || `${props.imageWidth}x${props.imageHeight}`
}

function applyViewportInstant(to: { x: number; y: number; zoom: number }) {
    cancelViewportAnim()
    viewport.value = { ...viewport.value, ...to }
    changed()
}

function animateViewportTo(target: Viewport) {
    const key = mapViewportKey()
    const to = {
        x: target.x,
        y: target.y,
        zoom: target.zoom,
    }
    // Tween only when staying on the same map and not mid cross-fade.
    // Map switches jump straight to the end pose (coords are unrelated).
    const allowTween =
        Boolean(viewportAnimKey) &&
        viewportAnimKey === key &&
        imageReady.value &&
        !previousSnapshot.value &&
        fadeIn.value >= 1
    viewportAnimKey = key
    if (!allowTween) return applyViewportInstant(to)
    cancelViewportAnim()
    const from = {
        x: viewport.value.x,
        y: viewport.value.y,
        zoom: viewport.value.zoom,
    }
    if (
        Math.abs(from.x - to.x) < 0.01 &&
        Math.abs(from.y - to.y) < 0.01 &&
        Math.abs(from.zoom - to.zoom) < 0.0001
    ) {
        return applyViewportInstant(to)
    }
    const start = performance.now()
    const step = (now: number) => {
        const t = Math.min(1, (now - start) / TRANSITION_MS)
        const e = easeOutCubic(t)
        viewport.value = {
            ...viewport.value,
            x: from.x + (to.x - from.x) * e,
            y: from.y + (to.y - from.y) * e,
            zoom: from.zoom + (to.zoom - from.zoom) * e,
        }
        changed()
        if (t < 1) {
            viewportAnimId = requestAnimationFrame(step)
            return
        }
        viewportAnimId = 0
        viewport.value = { ...viewport.value, ...to }
        changed()
    }
    viewportAnimId = requestAnimationFrame(step)
}

function startCrossfade() {
    cancelFadeAnim()
    fadeIn.value = 0
    const start = performance.now()
    const step = (now: number) => {
        const t = Math.min(1, (now - start) / TRANSITION_MS)
        fadeIn.value = easeOutCubic(t)
        if (t < 1) {
            fadeAnimId = requestAnimationFrame(step)
            return
        }
        fadeAnimId = 0
        fadeIn.value = 1
        previousSnapshot.value = null
    }
    fadeAnimId = requestAnimationFrame(step)
}

function snapshotCurrent() {
    if (!imageReady.value || !displayImage.complete || !displayImage.naturalWidth) return
    previousSnapshot.value = {
        image: displayImage,
        width: displayWidth,
        height: displayHeight,
        viewport: { ...viewport.value },
    }
    fadeIn.value = 0
}

function drawMapImage(
    context: CanvasRenderingContext2D,
    source: HTMLImageElement,
    width: number,
    height: number,
    view: Viewport,
    alpha: number,
) {
    if (alpha <= 0) return
    context.globalAlpha = alpha
    // Nearest when zoomed in (keep pixel edges); smooth when downscaling.
    context.imageSmoothingEnabled = view.zoom < 1
    context.drawImage(source, -view.x * view.zoom, -view.y * view.zoom, width * view.zoom, height * view.zoom)
    context.globalAlpha = 1
}

function loadImage() {
    cancelFadeAnim()
    imageError.value = false
    if (!props.imageUrl) {
        imageReady.value = false
        imageLoading.value = false
        previousSnapshot.value = null
        fadeIn.value = 1
        return
    }
    snapshotCurrent()
    imageLoading.value = true
    if (!previousSnapshot.value) imageReady.value = false
    const token = ++loadToken
    const next = new Image()
    next.onload = () => {
        if (token !== loadToken) return
        displayImage = next
        displayWidth = Math.max(1, props.imageWidth)
        displayHeight = Math.max(1, props.imageHeight)
        imageReady.value = true
        imageLoading.value = false
        if (previousSnapshot.value) startCrossfade()
        else fadeIn.value = 1
        const key = props.viewportKey || `${displayWidth}x${displayHeight}`
        if (fittedKey !== key) {
            fittedKey = key
            if (props.focusPoint) focus()
            else fit()
            emit('ready')
        }
    }
    next.onerror = () => {
        if (token !== loadToken) return
        imageLoading.value = false
        imageError.value = true
        if (!previousSnapshot.value) imageReady.value = false
    }
    next.src = props.imageUrl
}

function resize() {
    if (!host.value || !canvas.value) return
    const rect = host.value.getBoundingClientRect()
    const ratio = window.devicePixelRatio || 1
    canvas.value.width = Math.max(1, Math.round(rect.width * ratio))
    canvas.value.height = Math.max(1, Math.round(rect.height * ratio))
    viewport.value.width = rect.width
    viewport.value.height = rect.height
    if (imageReady.value && props.focusPoint) focus()
    else changed()
}

function draw() {
    const target = canvas.value
    if (!target) return
    const ratio = window.devicePixelRatio || 1
    const context = target.getContext('2d')
    if (!context) return
    context.setTransform(ratio, 0, 0, ratio, 0, 0)
    context.clearRect(0, 0, viewport.value.width, viewport.value.height)
    context.fillStyle = '#171717'
    context.fillRect(0, 0, viewport.value.width, viewport.value.height)
    const previous = previousSnapshot.value
    if (previous) {
        drawMapImage(
            context,
            previous.image,
            previous.width,
            previous.height,
            previous.viewport,
            1 - fadeIn.value,
        )
    }
    if (imageReady.value) {
        drawMapImage(
            context,
            displayImage,
            displayWidth,
            displayHeight,
            viewport.value,
            previous ? fadeIn.value : 1,
        )
    }
    for (const layer of sortedLayers.value) {
        if (layer.visible !== false) layer.draw(context, viewport.value)
    }
}

function fit() {
    const width = Math.max(1, props.imageWidth)
    const height = Math.max(1, props.imageHeight)
    fitTo(
        [
            [0, 0],
            [width, height],
        ],
        { padding: 0.02 },
    )
}

function fitTo(points: Point2[], options?: FitToOptions) {
    if (!points.length) return
    const next = fitViewportTo(
        viewport.value,
        points,
        { minZoom: props.minZoom, maxZoom: props.maxZoom },
        options,
    )
    animateViewportTo(next)
}

function focus(point = props.focusPoint) {
    if (!point) return
    const zoom =
        Math.min(viewport.value.width / props.focusWidth, viewport.value.height / props.focusHeight) * 0.92
    const clamped = Math.max(props.minZoom, Math.min(props.maxZoom, zoom))
    animateViewportTo({
        ...viewport.value,
        zoom: clamped,
        x: point[0] - viewport.value.width / clamped / 2,
        y: point[1] - viewport.value.height / clamped / 2,
    })
}

function maybeCenterTo(x: number, y: number, padding = 0.3) {
    animateViewportTo(maybeCenterViewport(viewport.value, x, y, padding))
}

defineExpose({ fit, fitTo, focus, maybeCenterTo })

function changed() {
    emit('viewport', { ...viewport.value })
}

function changeTier(offset: -1 | 1) {
    const option = props.tierOptions[tierIndex.value + offset]
    if (option) emit('update:tierValue', option.value)
}

function updateCrosshair(event: PointerEvent) {
    if (!host.value) return
    const rect = host.value.getBoundingClientRect()
    const x = event.clientX - rect.left
    const y = event.clientY - rect.top
    pointerScreen.value = [x, y]
    pointerVisible.value = x >= 0 && x <= rect.width && y >= 0 && y <= rect.height
}

function pointer(event: PointerEvent): MapPointerInfo {
    const rect = canvas.value!.getBoundingClientRect()
    const screenX = event.clientX - rect.left
    const screenY = event.clientY - rect.top
    return {
        x: screenX / viewport.value.zoom + viewport.value.x,
        y: screenY / viewport.value.zoom + viewport.value.y,
        screenX,
        screenY,
        button: event.button,
        buttons: event.buttons,
        viewport: { ...viewport.value },
    }
}

function onPointerDown(event: PointerEvent) {
    canvas.value?.setPointerCapture(event.pointerId)
    dragging.value = true
    if (event.button === 1 || event.button === 2) {
        cancelViewportAnim()
        panning.value = true
        panStart = [
            event.clientX,
            event.clientY,
            viewport.value.x,
            viewport.value.y,
        ]
        event.preventDefault()
        return
    }
    const info = pointer(event)
    pointerLocation.value = [info.x, info.y]
    emit('pointerdown', info)
}

function onPointerMove(event: PointerEvent) {
    if (panning.value) {
        viewport.value.x = panStart[2] - (event.clientX - panStart[0]) / viewport.value.zoom
        viewport.value.y = panStart[3] - (event.clientY - panStart[1]) / viewport.value.zoom
        return changed()
    }
    const info = pointer(event)
    pointerLocation.value = [info.x, info.y]
    emit('pointermove', info)
}

function onPointerUp(event: PointerEvent) {
    dragging.value = false
    if (panning.value) {
        panning.value = false
        return
    }
    const info = pointer(event)
    pointerLocation.value = [info.x, info.y]
    emit('pointerup', info)
}

function onWheel(event: WheelEvent) {
    event.preventDefault()
    cancelViewportAnim()
    const rect = canvas.value!.getBoundingClientRect()
    const screenX = event.clientX - rect.left
    const screenY = event.clientY - rect.top
    const mapX = screenX / viewport.value.zoom + viewport.value.x
    const mapY = screenY / viewport.value.zoom + viewport.value.y
    const factor = event.deltaY < 0 ? 1.14514 : 1 / 1.14514
    viewport.value.zoom = Math.max(props.minZoom, Math.min(props.maxZoom, viewport.value.zoom * factor))
    viewport.value.x = mapX - screenX / viewport.value.zoom
    viewport.value.y = mapY - screenY / viewport.value.zoom
    changed()
}

watch(() => props.imageUrl, loadImage)
watch(
    [
        () => props.focusPoint?.[0],
        () => props.focusPoint?.[1],
        () => props.focusWidth,
        () => props.focusHeight,
    ],
    () => focus(),
)
watchEffect(draw, { flush: 'post' })
onMounted(() => {
    resizeObserver = new ResizeObserver(resize)
    resizeObserver.observe(host.value!)
    nextTick(loadImage)
})
onBeforeUnmount(() => {
    resizeObserver?.disconnect()
    cancelViewportAnim()
    cancelFadeAnim()
})
</script>

<template>
    <div class="flex h-full min-h-0 w-full flex-col">
        <div
            ref="host"
            class="relative min-h-48 flex-1 overflow-hidden bg-neutral-900 md:min-h-80"
            :class="cursorClass"
            @pointermove="updateCrosshair"
            @pointerleave="pointerVisible = false"
            @contextmenu.prevent>
            <canvas
                ref="canvas"
                class="block h-full w-full touch-none"
                @pointerdown="onPointerDown"
                @pointermove="onPointerMove"
                @pointerup="onPointerUp"
                @pointercancel="onPointerUp"
                @wheel="onWheel" />
            <div v-if="showLoading" class="absolute inset-0 grid place-items-center">
                <el-icon class="is-loading text-white"><Loading /></el-icon>
            </div>
            <el-empty
                v-else-if="(!imageUrl || imageError) && !previousSnapshot"
                class="absolute inset-0 bg-neutral-900"
                :description="imageError ? t('canvas.loadFailed') : t('canvas.noMap')"
                :image-size="0" />
            <el-button-group v-if="tierOptions.length" class="absolute right-3 bottom-3 z-10">
                <el-button
                    :icon="Minus"
                    :disabled="tierDisabled || tierIndex <= 0"
                    :title="t('canvas.previousTier')"
                    @click="changeTier(-1)" />
                <el-tooltip :content="currentTier?.label || t('canvas.tier')" placement="top">
                    <el-button class="pointer-events-none w-24" tabindex="-1">
                        <span class="max-w-20 truncate">{{ currentTier?.label || '-' }}</span>
                    </el-button>
                </el-tooltip>
                <el-button
                    :icon="Plus"
                    :disabled="tierDisabled || tierIndex < 0 || tierIndex >= tierOptions.length - 1"
                    :title="t('canvas.nextTier')"
                    @click="changeTier(1)" />
            </el-button-group>
            <div v-if="pointerVisible" class="pointer-events-none absolute inset-0 z-20 overflow-hidden">
                <div
                    class="absolute right-0 left-0 h-px bg-yellow-400/50"
                    :style="{ top: `${pointerScreen[1]}px` }" />
                <div
                    class="absolute top-0 bottom-0 w-px bg-yellow-400/50"
                    :style="{ left: `${pointerScreen[0]}px` }" />
            </div>
            <slot name="overlay" />
        </div>
        <slot name="footer" />
        <div
            class="flex h-8 shrink-0 items-center gap-4 border-t border-(--el-border-color) px-3 text-xs text-(--el-text-color-secondary)">
            <slot name="status" />
            <span class="ml-auto font-mono">
                {{ pointerLocation[0].toFixed(1) }}, {{ pointerLocation[1].toFixed(1) }} ·
                {{ viewport.zoom.toFixed(2) }}x
            </span>
            <el-tooltip :content="t('canvas.fit')" placement="top">
                <el-button link :icon="FullScreen" @click="fit" />
            </el-tooltip>
        </div>
    </div>
</template>
