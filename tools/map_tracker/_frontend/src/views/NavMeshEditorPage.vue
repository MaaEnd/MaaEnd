<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CheckboxValueType } from 'element-plus'
import {
    Aim,
    ArrowUp,
    Connection,
    CopyDocument,
    Delete,
    FolderOpened,
    InfoFilled,
    Plus,
    RefreshLeft,
    RefreshRight,
    Switch,
    VideoPause,
    VideoPlay,
} from '@element-plus/icons-vue'
import { MouseLeft, MouseRight, Save } from '@lucide/vue'
import MapCanvas from '@/components/MapCanvas.vue'
import {
    distanceToSegment,
    drawMapCrosshair,
    mapToScreen,
    type MapLayer,
    type MapPointerInfo,
    type Viewport,
} from '@/utils/canvas'
import { useUndoHistory } from '@/composables/useUndoHistory'
import { useUnsavedChangesGuard } from '@/composables/useUnsavedChangesGuard'
import { api } from '@/api'
import { NavMeshRecorder } from '@/utils/navmeshRecorder'
import type { MapInfo, NavEdge, NavMeshDocument, NavVertex, Point } from '@/types'
import {
    errorMessage,
    findLayerByName,
    isInteractiveTarget,
    mapTierOptions,
    readLocalFlag,
    tierIdOf,
    writeLocalFlag,
} from '@/utils/misc'

type Selection = { type: 'vertex' | 'edge'; id: number } | null
type EditTool = 'vertex' | 'edge'
interface GraphSnapshot {
    vertices: NavVertex[]
    edges: NavEdge[]
}
type MapCanvasExpose = {
    maybeCenterTo: (x: number, y: number, padding?: number) => void
}

const FLAG_TELEPORT = 1
const FLAG_HIDDEN = 2
const FLAG_SYSTEM = 4
const FLAG_RARE = 8
const FLAG_COLLECTABLE = 16
const FLAG_DIG = 32
const EDGE_BIDIRECTIONAL = 1
const GUIDE_EXPANDED_KEY = 'maptracker.navmesh.guide.expanded'
const { t } = useI18n()
const flagOptions = computed(() => [
    { label: t('navmesh.flag.teleport'), value: FLAG_TELEPORT },
    { label: t('navmesh.flag.hidden'), value: FLAG_HIDDEN },
    { label: t('navmesh.flag.system'), value: FLAG_SYSTEM },
    { label: t('navmesh.flag.rare'), value: FLAG_RARE },
    { label: t('navmesh.flag.collectable'), value: FLAG_COLLECTABLE },
    { label: t('navmesh.flag.dig'), value: FLAG_DIG },
])
const editToolOptions = computed(() => [
    { label: t('common.addPoint'), value: 'vertex', icon: Plus },
    { label: t('navmesh.addEdge'), value: 'edge', icon: Connection },
])

const maps = ref<MapInfo[]>([])
const files = ref<string[]>([])
const document = ref<NavMeshDocument | null>(null)
const cleanState = ref('')
const selection = ref<Selection>(null)
const editTool = ref<EditTool>('vertex')
const mouse = ref<Point>([
    0,
    0,
])
const openDialog = ref(true)
const dialogTab = ref<'existing' | 'new'>('existing')
const selectedFile = ref('')
const selectedMapId = ref('')
const selectedLayerFile = ref('')
const sidebarTab = ref('document')
const linkSource = ref<number | null>(null)
const recording = ref(false)
const lastRecordPosition = ref<Point | null>(null)
const saving = ref(false)
const opening = ref(false)
const goalRunning = ref(false)
const requestError = ref('')
const guideExpanded = ref(readLocalFlag(GUIDE_EXPANDED_KEY, true))
const mapCanvasRef = ref<MapCanvasExpose>()
let movingVertex = -1
let moveChanged = false
let recordingGeneration = 0
let recordingTimer: number | undefined
let recorder: NavMeshRecorder | null = null

const activeMap = computed(() => {
    if (!document.value) return undefined
    return maps.value.find((map) => map.layers.some((layer) => layer.file_name === document.value!.map_file))
})
const tierOptions = computed(() => mapTierOptions(activeMap.value, t('canvas.baseTier')))
const imageUrl = computed(() => (selectedLayerFile.value ? api.mapImage(selectedLayerFile.value) : ''))
const vertexIndex = computed(
    () =>
        new Map(
            document.value?.vertices.map((item) => [
                item.id,
                item,
            ]) ?? [],
        ),
)
const selectedVertex = computed(() =>
    selection.value?.type === 'vertex' ? vertexIndex.value.get(selection.value.id) : undefined,
)
const selectedEdge = computed(() =>
    selection.value?.type === 'edge'
        ? document.value?.edges.find((edge) => edge.id === selection.value!.id)
        : undefined,
)
const isDirty = computed(() => Boolean(document.value) && serializeDocument(document.value!) !== cleanState.value)
const { confirmDiscard } = useUnsavedChangesGuard(isDirty)
const {
    history,
    future,
    snapshot,
    undo,
    redo,
    clear: clearHistory,
    discardSnapshot,
} = useUndoHistory(captureSnapshot, applySnapshot, () => recording.value)
const selectedFlags = computed(() =>
    flagOptions.value
        .filter((option) => Boolean((selectedVertex.value?.flags ?? 0) & option.value))
        .map((option) => option.value),
)
const selectedEdgeLength = computed(() => {
    if (!selectedEdge.value) return 0
    const source = vertex(selectedEdge.value.from_id)
    const target = vertex(selectedEdge.value.to_id)
    return source && target ? Math.hypot(source.x - target.x, source.y - target.y) : 0
})

function serializeDocument(value: NavMeshDocument) {
    return JSON.stringify({ meta: value.meta, vertices: value.vertices, edges: value.edges })
}

function captureSnapshot(): GraphSnapshot | undefined {
    if (!document.value) return
    return {
        vertices: document.value.vertices.map((vertex) => ({ ...vertex })),
        edges: document.value.edges.map((edge) => ({ ...edge })),
    }
}

function applySnapshot(state: GraphSnapshot) {
    if (!document.value) return
    document.value.vertices = state.vertices.map((vertex) => ({ ...vertex }))
    document.value.edges = state.edges.map((edge) => ({ ...edge }))
    selection.value = null
    linkSource.value = null
}

function vertex(id: number) {
    return vertexIndex.value.get(id)
}

function nextId(items: Array<{ id: number }>) {
    return items.reduce((maximum, item) => Math.max(maximum, item.id), 0) + 1
}

function vertexAt(event: MapPointerInfo) {
    return document.value?.vertices.find((item) => {
        const [
            x,
            y,
        ] = mapToScreen(event.viewport, item.x, item.y)
        return Math.hypot(event.screenX - x, event.screenY - y) <= 8
    })
}

function edgeAt(event: MapPointerInfo) {
    return document.value?.edges.find((item) => {
        const source = vertex(item.from_id)
        const target = vertex(item.to_id)
        if (!source || !target) return false
        const [
            x1,
            y1,
        ] = mapToScreen(event.viewport, source.x, source.y)
        const [
            x2,
            y2,
        ] = mapToScreen(event.viewport, target.x, target.y)
        return distanceToSegment(event.screenX, event.screenY, x1, y1, x2, y2) < 8
    })
}

function drawVertex(context: CanvasRenderingContext2D, view: Viewport, item: NavVertex) {
    const [
        x,
        y,
    ] = mapToScreen(view, item.x, item.y)
    const size = Math.max(3, Math.sqrt(view.zoom) * 3)
    context.fillStyle =
        selection.value?.type === 'vertex' && selection.value.id === item.id
            ? '#f56c6c'
            : item.entity_id
              ? '#e6a23c'
              : item.flags & FLAG_HIDDEN
                ? '#909399'
                : '#f2f3f5'
    context.strokeStyle = '#303133'
    context.lineWidth = 1.5
    context.beginPath()
    if (item.flags & FLAG_TELEPORT) context.rect(x - size, y - size, size * 2, size * 2)
    else if (item.flags & FLAG_SYSTEM) {
        context.moveTo(x, y - size * 1.3)
        context.lineTo(x - size, y + size)
        context.lineTo(x + size, y + size)
        context.closePath()
    } else context.arc(x, y, size, 0, Math.PI * 2)
    context.fill()
    context.stroke()
}

const graphLayer = {
    id: 'navmesh',
    zIndex: 20,
    draw(context, view) {
        if (!document.value) return
        context.save()
        context.lineCap = 'butt'
        context.lineWidth = Math.max(1.3, Math.sqrt(view.zoom) * 1.3)
        for (const edge of document.value.edges) {
            const source = vertex(edge.from_id)
            const target = vertex(edge.to_id)
            if (!source || !target) continue
            const [
                x1,
                y1,
            ] = mapToScreen(view, source.x, source.y)
            const [
                x2,
                y2,
            ] = mapToScreen(view, target.x, target.y)
            context.strokeStyle =
                selection.value?.type === 'edge' && selection.value.id === edge.id ? '#f56c6c' : '#e6a23c'
            context.beginPath()
            context.moveTo(x1, y1)
            context.lineTo(x2, y2)
            context.stroke()
            if (!(edge.flags & EDGE_BIDIRECTIONAL)) {
                const angle = Math.atan2(y2 - y1, x2 - x1)
                const middleX = (x1 + x2) / 2
                const middleY = (y1 + y2) / 2
                const arrowSize = Math.max(5, Math.sqrt(view.zoom) * 6)
                context.fillStyle = context.strokeStyle
                context.beginPath()
                context.moveTo(middleX + Math.cos(angle) * arrowSize, middleY + Math.sin(angle) * arrowSize)
                context.lineTo(
                    middleX + Math.cos(angle + 2.5) * arrowSize,
                    middleY + Math.sin(angle + 2.5) * arrowSize,
                )
                context.lineTo(
                    middleX + Math.cos(angle - 2.5) * arrowSize,
                    middleY + Math.sin(angle - 2.5) * arrowSize,
                )
                context.fill()
            }
        }
        document.value.vertices.forEach((item) => drawVertex(context, view, item))
        if (linkSource.value !== null) {
            const source = vertex(linkSource.value)
            if (source) {
                const [
                    x1,
                    y1,
                ] = mapToScreen(view, source.x, source.y)
                const [
                    x2,
                    y2,
                ] = mapToScreen(view, mouse.value[0], mouse.value[1])
                context.setLineDash([
                    7,
                    6,
                ])
                context.strokeStyle = '#409eff'
                context.beginPath()
                context.moveTo(x1, y1)
                context.lineTo(x2, y2)
                context.stroke()
            }
        }
        context.restore()
    },
} satisfies MapLayer

const recordCrosshairLayer = {
    id: 'record-crosshair',
    zIndex: 30,
    draw(context, view) {
        const point = lastRecordPosition.value
        if (!recording.value || !point) return
        drawMapCrosshair(context, view, point[0], point[1])
    },
} satisfies MapLayer

function hasEdge(from: number, to: number) {
    return document.value?.edges.some(
        (edge) => (edge.from_id === from && edge.to_id === to) || (edge.from_id === to && edge.to_id === from),
    )
}

function createEdge(from: number, to: number) {
    if (!document.value || from === to || hasEdge(from, to)) return false
    const id = nextId(document.value.edges)
    document.value.edges.push({ id, flags: EDGE_BIDIRECTIONAL, from_id: from, to_id: to, cost: 0 })
    return true
}

function beginPointer(event: MapPointerInfo) {
    if (!document.value || recording.value || event.button !== 0) return
    const hitVertex = vertexAt(event)
    if (editTool.value === 'edge') {
        if (hitVertex) {
            selection.value = { type: 'vertex', id: hitVertex.id }
            sidebarTab.value = 'selection'
            if (linkSource.value === null) linkSource.value = hitVertex.id
            else {
                snapshot()
                if (!createEdge(linkSource.value, hitVertex.id)) discardSnapshot()
                linkSource.value = null
            }
            return
        }
        const hitEdge = edgeAt(event)
        linkSource.value = null
        selection.value = hitEdge ? { type: 'edge', id: hitEdge.id } : null
        if (hitEdge) sidebarTab.value = 'selection'
        return
    }
    if (hitVertex) {
        selection.value = { type: 'vertex', id: hitVertex.id }
        sidebarTab.value = 'selection'
        if (!hitVertex.entity_id) movingVertex = hitVertex.id
        return
    }
    const hitEdge = edgeAt(event)
    if (hitEdge) {
        selection.value = { type: 'edge', id: hitEdge.id }
        sidebarTab.value = 'selection'
        return
    }
    snapshot()
    const id = nextId(document.value.vertices)
    document.value.vertices.push({
        id,
        flags: 0,
        x: +event.x.toFixed(3),
        y: +event.y.toFixed(3),
        entity_id: 0,
        tier_id: tierId(),
    })
    selection.value = { type: 'vertex', id }
    sidebarTab.value = 'selection'
}

function movePointer(event: MapPointerInfo) {
    mouse.value = [
        +event.x.toFixed(1),
        +event.y.toFixed(1),
    ]
    if (movingVertex < 0 || event.buttons !== 1 || recording.value) return
    const item = vertex(movingVertex)
    if (!item) return
    if (!moveChanged) snapshot()
    moveChanged = true
    item.x = +event.x.toFixed(3)
    item.y = +event.y.toFixed(3)
}

function endPointer() {
    movingVertex = -1
    moveChanged = false
}

function deleteSelection() {
    if (!document.value || !selection.value || recording.value) return
    if (selectedVertex.value?.entity_id) return void ElMessage.warning(t('navmesh.entityReadonly'))
    snapshot()
    if (selection.value.type === 'vertex') {
        document.value.vertices = document.value.vertices.filter((item) => item.id !== selection.value!.id)
        document.value.edges = document.value.edges.filter(
            (edge) => edge.from_id !== selection.value!.id && edge.to_id !== selection.value!.id,
        )
    } else document.value.edges = document.value.edges.filter((edge) => edge.id !== selection.value!.id)
    selection.value = null
}

function toggleEditTool() {
    if (recording.value) return
    editTool.value = editTool.value === 'vertex' ? 'edge' : 'vertex'
    linkSource.value = null
}

function onKeydown(event: KeyboardEvent) {
    if (!document.value || isInteractiveTarget(event.target)) return
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'z') {
        event.preventDefault()
        event.shiftKey ? redo() : undo()
    } else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'y') {
        event.preventDefault()
        redo()
    } else if (!event.ctrlKey && !event.metaKey && !event.altKey && event.key === 'Delete') {
        event.preventDefault()
        deleteSelection()
    } else if (!event.ctrlKey && !event.metaKey && !event.altKey && event.code === 'Space') {
        event.preventDefault()
        toggleEditTool()
    }
}

function updateVertex(field: 'x' | 'y' | 'tier_id', value: number | undefined) {
    if (!selectedVertex.value || selectedVertex.value.entity_id || value === undefined) return
    snapshot()
    selectedVertex.value[field] = field === 'tier_id' ? Math.trunc(value) : +value.toFixed(3)
}

async function copySelectedVertex() {
    const vertex = selectedVertex.value
    if (!vertex) return
    await navigator.clipboard.writeText(JSON.stringify([vertex.x, vertex.y]))
    ElMessage.success(t('common.copied'))
}

function updateFlags(values: CheckboxValueType[]) {
    if (!selectedVertex.value || selectedVertex.value.entity_id) return
    snapshot()
    selectedVertex.value.flags = values.reduce<number>((flags, value) => flags | Number(value), 0)
}

function toggleDirection(value: string | number | boolean) {
    if (!selectedEdge.value) return
    snapshot()
    selectedEdge.value.flags = value ? EDGE_BIDIRECTIONAL : 0
}

function reverseEdge() {
    if (!selectedEdge.value || selectedEdge.value.flags & EDGE_BIDIRECTIONAL) return
    snapshot()
    ;[
        selectedEdge.value.from_id,
        selectedEdge.value.to_id,
    ] = [
        selectedEdge.value.to_id,
        selectedEdge.value.from_id,
    ]
}

function tierId() {
    return tierIdOf(selectedLayerFile.value)
}

async function loadExisting() {
    if (saving.value || !selectedFile.value) return
    opening.value = true
    try {
        setDocument(await api.loadNavmesh(selectedFile.value), false)
    } catch (error) {
        ElMessage.error(errorMessage(error))
    } finally {
        opening.value = false
    }
}

async function createNew() {
    if (saving.value) return
    const map = maps.value.find((item) => item.id === selectedMapId.value)
    const layer = map?.layers.find((item) => item.id === 'main') || map?.layers[0]
    if (!layer) return
    opening.value = true
    try {
        const result = await api.newNavmesh(layer.file_name)
        setDocument(result, true)
    } catch (error) {
        ElMessage.error(errorMessage(error))
    } finally {
        opening.value = false
    }
}

function setDocument(value: NavMeshDocument, created: boolean) {
    document.value = value
    selection.value = null
    linkSource.value = null
    clearHistory()
    openDialog.value = false
    const map = maps.value.find((item) => item.layers.some((layer) => layer.file_name === value.map_file))
    selectedMapId.value = map?.id || ''
    selectedLayerFile.value = value.map_file
    cleanState.value = created ? '' : serializeDocument(value)
}

async function save() {
    const currentDocument = document.value
    if (!currentDocument || recording.value || saving.value) return
    const savedDocument: NavMeshDocument = {
        ...currentDocument,
        meta: { ...currentDocument.meta },
        vertices: currentDocument.vertices.map((vertex) => ({ ...vertex })),
        edges: currentDocument.edges.map((edge) => ({ ...edge })),
    }
    const savedState = serializeDocument(savedDocument)
    saving.value = true
    try {
        const result = await api.saveNavmesh(savedDocument)
        if (document.value && document.value === currentDocument) {
            document.value.revision = result.revision
            cleanState.value = savedState
        }
        if (!files.value.includes(savedDocument.name)) files.value.push(savedDocument.name)
        ElMessage.success(t('common.saved'))
    } catch (error) {
        ElMessage.error(errorMessage(error))
    } finally {
        saving.value = false
    }
}

async function openAnother() {
    if (saving.value || !(await confirmDiscard())) return
    stopRecording()
    openDialog.value = true
}

function acceptLocation(x: number, y: number, inferredMap: string) {
    if (!document.value || !recorder) return
    snapshot()
    const tierMatch = inferredMap.match(/_tier_(\d+)/)
    const currentTier = tierMatch ? Number(tierMatch[1]) : 0
    const result = recorder.acceptLocation(x, y, 0, currentTier)
    if (!result.dirty) discardSnapshot()
    lastRecordPosition.value = result.location
    const layer = findLayerByName(activeMap.value, inferredMap)
    if (layer) selectedLayerFile.value = layer.file_name
    mapCanvasRef.value?.maybeCenterTo(x, y)
}

async function recordStep(generation: number, documentName: string) {
    if (!document.value || generation !== recordingGeneration || document.value.name !== documentName) return
    try {
        const result = await api.infer(document.value.meta.name)
        if (generation !== recordingGeneration || document.value?.name !== documentName) return
        requestError.value = ''
        acceptLocation(result.x, result.y, result.map_name)
    } catch (error) {
        if (generation === recordingGeneration) requestError.value = errorMessage(error)
        recorder?.recordError()
        lastRecordPosition.value = null
    }
    if (recording.value && generation === recordingGeneration) {
        recordingTimer = window.setTimeout(() => recordStep(generation, documentName), 300)
    }
}

function startRecording() {
    if (!document.value) return
    recording.value = true
    linkSource.value = null
    lastRecordPosition.value = null
    requestError.value = ''
    recorder = new NavMeshRecorder(document.value)
    recorder.start()
    const generation = ++recordingGeneration
    recordStep(generation, document.value.name)
}

function stopRecording() {
    recording.value = false
    recordingGeneration += 1
    window.clearTimeout(recordingTimer)
    recorder?.stop()
    recorder = null
    lastRecordPosition.value = null
}

async function testGoal() {
    if (!document.value || !selectedVertex.value || recording.value) return
    goalRunning.value = true
    requestError.value = ''
    try {
        await api.goal(document.value.meta.name, selectedVertex.value.x, selectedVertex.value.y)
        ElMessage.success(t('navmesh.goalComplete'))
    } catch (error) {
        requestError.value = errorMessage(error)
    } finally {
        goalRunning.value = false
    }
}

async function loadData() {
    try {
        ;[
            maps.value,
            files.value,
        ] = await Promise.all([
            api.maps(),
            api.navmeshes(),
        ])
    } catch (error) {
        ElMessage.error(errorMessage(error))
    }
}

watch(guideExpanded, (expanded) => writeLocalFlag(GUIDE_EXPANDED_KEY, expanded))

onMounted(() => {
    window.addEventListener('keydown', onKeydown)
    void loadData()
})
onBeforeUnmount(() => {
    stopRecording()
    window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
    <div class="flex h-full min-h-0 flex-col md:flex-row">
        <aside
            v-if="document"
            class="max-h-72 w-full shrink-0 overflow-auto border-b border-(--el-border-color) md:max-h-none md:w-80 md:border-r md:border-b-0">
            <div class="flex items-center gap-2 border-b border-(--el-border-color) p-3">
                <el-button :icon="FolderOpened" @click="openAnother">{{ t('common.open') }}</el-button>
                <el-button
                    type="primary"
                    :icon="Save"
                    :loading="saving"
                    :disabled="!isDirty || recording"
                    @click="save">
                    {{ t('common.save') }}
                </el-button>
            </div>
            <el-tabs v-model="sidebarTab" class="px-3">
                <el-tab-pane :label="t('navmesh.document')" name="document">
                    <el-form label-position="top">
                        <el-form-item :label="t('common.name')"
                            ><el-input v-model="document.meta.name" :disabled="recording"
                        /></el-form-item>
                        <el-form-item :label="t('common.description')"
                            ><el-input v-model="document.meta.description" :disabled="recording"
                        /></el-form-item>
                        <el-form-item :label="t('common.region')"
                            ><el-input :model-value="document.meta.map_region_name" disabled
                        /></el-form-item>
                        <el-form-item :label="t('common.tier')"
                            ><el-input :model-value="document.meta.map_level_name" disabled
                        /></el-form-item>
                    </el-form>
                </el-tab-pane>
                <el-tab-pane :label="t('common.properties')" name="selection">
                    <el-empty
                        v-if="!selectedVertex && !selectedEdge"
                        :image-size="0"
                        :description="t('navmesh.noSelection')" />
                    <el-form v-else-if="selectedVertex" label-position="top">
                        <div class="grid grid-cols-2 gap-x-3">
                            <el-form-item :label="t('node.xCoord')"
                                ><el-input-number
                                    class="w-full!"
                                    :disabled="recording || Boolean(selectedVertex.entity_id)"
                                    :model-value="selectedVertex.x"
                                    :precision="3"
                                    controls-position="right"
                                    @change="updateVertex('x', $event)"
                            /></el-form-item>
                            <el-form-item :label="t('node.yCoord')"
                                ><el-input-number
                                    class="w-full!"
                                    :disabled="recording || Boolean(selectedVertex.entity_id)"
                                    :model-value="selectedVertex.y"
                                    :precision="3"
                                    controls-position="right"
                                    @change="updateVertex('y', $event)"
                            /></el-form-item>
                        </div>
                        <el-button class="mb-3 w-full" :icon="CopyDocument" @click="copySelectedVertex">
                            {{ t('node.copyCoordinates') }}
                        </el-button>
                        <el-form-item :label="t('common.tier')"
                            ><el-input-number
                                class="w-full!"
                                :disabled="recording || Boolean(selectedVertex.entity_id)"
                                :model-value="selectedVertex.tier_id"
                                :precision="0"
                                controls-position="right"
                                @change="updateVertex('tier_id', $event)"
                        /></el-form-item>
                        <el-form-item :label="t('navmesh.flags')"
                            ><el-checkbox-group
                                :model-value="selectedFlags"
                                :disabled="recording || Boolean(selectedVertex.entity_id)"
                                @change="updateFlags"
                                ><el-checkbox v-for="option in flagOptions" :key="option.value" :value="option.value">{{
                                    option.label
                                }}</el-checkbox></el-checkbox-group
                            ></el-form-item
                        >
                        <el-form-item v-if="selectedVertex.entity_id" :label="t('navmesh.entity')"
                            ><el-input :model-value="String(selectedVertex.entity_id)" disabled
                        /></el-form-item>
                        <div class="flex gap-2">
                            <el-button :icon="Aim" :loading="goalRunning" :disabled="recording" @click="testGoal">{{
                                t('navmesh.testGoal')
                            }}</el-button>
                            <el-button
                                type="danger"
                                plain
                                :icon="Delete"
                                :disabled="Boolean(selectedVertex.entity_id)"
                                @click="deleteSelection"
                                >{{ t('common.delete') }}</el-button
                            >
                        </div>
                    </el-form>
                    <el-form v-else-if="selectedEdge" label-position="top" :disabled="recording">
                        <el-form-item :label="t('navmesh.endpoint')"
                            ><el-input :model-value="`V${selectedEdge.from_id} → V${selectedEdge.to_id}`" disabled
                        /></el-form-item>
                        <el-form-item :label="t('navmesh.length')"
                            ><el-input :model-value="selectedEdgeLength.toFixed(3)" disabled
                        /></el-form-item>
                        <el-form-item :label="t('navmesh.bidirectional')"
                            ><el-switch
                                :model-value="Boolean(selectedEdge.flags & EDGE_BIDIRECTIONAL)"
                                @change="toggleDirection"
                        /></el-form-item>
                        <div class="flex gap-2">
                            <el-button
                                :icon="Switch"
                                :disabled="Boolean(selectedEdge.flags & EDGE_BIDIRECTIONAL)"
                                @click="reverseEdge"
                                >{{ t('navmesh.reverse') }}</el-button
                            >
                            <el-button type="danger" plain :icon="Delete" @click="deleteSelection">{{
                                t('common.delete')
                            }}</el-button>
                        </div>
                    </el-form>
                </el-tab-pane>
            </el-tabs>
        </aside>

        <main class="flex min-w-0 flex-1 flex-col">
            <el-alert
                v-if="requestError"
                class="rounded-none! border-b border-(--el-color-danger-light-5)"
                type="error"
                :title="requestError"
                :closable="false"
                show-icon />
            <template v-if="document">
                <div class="flex min-h-12 flex-wrap items-center gap-2 border-b border-(--el-border-color) px-3 py-2">
                    <el-segmented
                        v-model="editTool"
                        :disabled="recording"
                        :options="editToolOptions"
                        @change="linkSource = null" />
                    <el-button-group>
                        <el-button
                            :icon="RefreshLeft"
                            :disabled="!history.length || recording"
                            :title="t('common.undo')"
                            @click="undo">
                            {{ t('common.undo') }}
                        </el-button>
                        <el-button
                            :icon="RefreshRight"
                            :disabled="!future.length || recording"
                            :title="t('common.redo')"
                            @click="redo">
                            {{ t('common.redo') }}
                        </el-button>
                    </el-button-group>
                    <span class="flex-1" />
                    <el-button
                        :type="recording ? 'danger' : 'default'"
                        :icon="recording ? VideoPause : VideoPlay"
                        @click="recording ? stopRecording() : startRecording()"
                        >{{ recording ? t('navmesh.stopRecording') : t('navmesh.record') }}</el-button
                    >
                </div>
                <div class="min-h-0 flex-1">
                    <MapCanvas
                        ref="mapCanvasRef"
                        v-model:tier-value="selectedLayerFile"
                        :image-url="imageUrl"
                        :image-width="activeMap?.width"
                        :image-height="activeMap?.height"
                        :viewport-key="activeMap?.id"
                        :layers="[graphLayer, recordCrosshairLayer]"
                        :tier-options="tierOptions"
                        :tier-disabled="recording"
                        @pointerdown="beginPointer"
                        @pointermove="movePointer"
                        @pointerup="endPointer">
                        <template #overlay>
                            <div
                                class="absolute top-3 right-3 z-10 max-w-72 rounded-md border border-white/20 bg-neutral-900 px-3 py-2 text-xs text-white shadow">
                                <div
                                    class="flex items-center gap-1 font-medium"
                                    :class="guideExpanded ? 'mb-2.5' : 'cursor-pointer'"
                                    @click="guideExpanded = true">
                                    <el-icon><InfoFilled /></el-icon>
                                    {{ t('node.guide.title') }}
                                </div>
                                <template v-if="guideExpanded">
                                    <ul class="space-y-1 text-white/90">
                                        <li class="flex items-start gap-2">
                                            <span
                                                class="inline-flex size-5 shrink-0 items-center justify-center rounded bg-white/15"
                                                :title="t('node.guide.rightClick')">
                                                <MouseRight class="size-3.5" />
                                            </span>
                                            <span>{{ t('node.guide.pan') }}</span>
                                        </li>
                                        <li class="flex items-start gap-2">
                                            <span
                                                class="inline-flex size-5 shrink-0 items-center justify-center rounded bg-white/15"
                                                :title="t('node.guide.leftClick')">
                                                <MouseLeft class="size-3.5" />
                                            </span>
                                            <span>{{ t('navmesh.guide.leftClick') }}</span>
                                        </li>
                                        <li class="flex items-start gap-2">
                                            <span class="shrink-0 rounded bg-white/15 px-1.5 py-0.5 font-mono"
                                                >Delete</span
                                            >
                                            <span>{{ t('navmesh.guide.delete') }}</span>
                                        </li>
                                        <li class="flex items-start gap-2">
                                            <span class="shrink-0 rounded bg-white/15 px-1.5 py-0.5 font-mono"
                                                >Space</span
                                            >
                                            <span>{{ t('navmesh.guide.toggleTool') }}</span>
                                        </li>
                                    </ul>
                                    <button
                                        type="button"
                                        class="mt-1.5 flex w-full cursor-pointer items-center justify-center text-white/70 hover:text-white"
                                        :title="t('node.guide.collapse')"
                                        @click="guideExpanded = false">
                                        <el-icon><ArrowUp /></el-icon>
                                    </button>
                                </template>
                            </div>
                        </template>
                        <template #status>
                            <el-text v-if="isDirty" type="warning" size="small">{{ t('common.unsaved') }}</el-text>
                            <span v-if="linkSource !== null">{{ t('navmesh.linkingFrom', { vertex: linkSource }) }}</span>
                            <span>V {{ document.vertices.length }} · E {{ document.edges.length }}</span>
                        </template>
                    </MapCanvas>
                </div>
            </template>
            <el-empty v-else class="h-full" :description="t('navmesh.noOpenDocument')">
                <el-button type="primary" :icon="FolderOpened" @click="openDialog = true">{{
                    t('common.open')
                }}</el-button>
            </el-empty>
        </main>

        <el-dialog
            v-model="openDialog"
            :title="t('routes.navmeshEditor')"
            width="94%"
            class="max-w-160"
            :close-on-click-modal="true"
            :close-on-press-escape="true"
            :show-close="true">
            <el-tabs v-model="dialogTab">
                <el-tab-pane :label="t('common.open')" name="existing">
                    <el-select v-model="selectedFile" filterable class="w-full" :placeholder="t('common.file')"
                        ><el-option v-for="file in files" :key="file" :label="file" :value="file"
                    /></el-select>
                </el-tab-pane>
                <el-tab-pane :label="t('navmesh.newDocument')" name="new">
                    <el-select v-model="selectedMapId" filterable class="w-full" :placeholder="t('common.map')"
                        ><el-option v-for="map in maps" :key="map.id" :label="map.name" :value="map.id"
                    /></el-select>
                </el-tab-pane>
            </el-tabs>
            <template #footer>
                <el-button
                    type="primary"
                    :icon="dialogTab === 'existing' ? FolderOpened : Plus"
                    :loading="opening"
                    :disabled="dialogTab === 'existing' ? !selectedFile : !selectedMapId"
                    @click="dialogTab === 'existing' ? loadExisting() : createNew()">
                    {{ dialogTab === 'existing' ? t('common.open') : t('common.create') }}
                </el-button>
            </template>
        </el-dialog>
    </div>
</template>
