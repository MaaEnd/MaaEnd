<script setup lang="ts">
import { computed, h, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import {
    ArrowLeft,
    ArrowRight,
    ArrowUp,
    Brush,
    CopyDocument,
    Delete,
    Document,
    Download,
    Folder,
    InfoFilled,
    Location,
    Plus,
    Refresh,
    RefreshLeft,
    RefreshRight,
    Search,
    VideoCamera,
    VideoPause,
} from '@element-plus/icons-vue'
import { BookOpen, MouseLeft, MouseRight, Save } from '@lucide/vue'
import type { ButtonInstance, FormItemInstance, TreeInstance, TreeNodeData } from 'element-plus'
import { ElButton, ElMessageBox } from 'element-plus'
import MapCanvas from '@/components/MapCanvas.vue'
import MapPicker from '@/components/MapPicker.vue'
import {
    distanceToSegment,
    drawMapCrosshair,
    mapToScreen,
    type MapLayer,
    type MapPointerInfo,
} from '@/utils/canvas'
import { useUndoHistory } from '@/composables/useUndoHistory'
import { useUnsavedChangesGuard } from '@/composables/useUnsavedChangesGuard'
import { api } from '@/api'
import { useNodeStore } from '@/stores/nodes'
import type { EditableNode, MapInfo, NodeType, PipelineFile, Point, Rect } from '@/types'
import {
    errorMessage,
    findLayerByName,
    findMapByName,
    isInteractiveTarget,
    mapTierOptions,
    readLocalFlag,
    stripMapExtension,
    writeLocalFlag,
} from '@/utils/misc'

type ExportTab = 'params' | 'node' | 'coordinates'
const GUIDE_EXPANDED_KEY = 'maptracker.guide.expanded'
const TOUR_RECORD_KEY = 'maptracker.tour.record.seen'
const TOUR_NEW_NODE_KEY = 'maptracker.tour.newNode.seen'
const TOUR_EXPORT_KEY = 'maptracker.tour.export.seen'
interface NodeSnapshot {
    path: Point[]
    target: Rect | null
}
interface PipelineTreeItem {
    id: string
    label: string
    displayLabel: string
    kind: 'directory' | 'file' | 'node'
    children?: PipelineTreeItem[]
    file?: PipelineFile
    node?: PipelineFile['nodes'][number]
}
type MapCanvasExpose = {
    fit: () => void
    fitTo: (
        points: Point[],
        options?: { padding?: number; minZoom?: number; maxZoom?: number },
    ) => void
    maybeCenterTo: (x: number, y: number, padding?: number) => void
}

const route = useRoute()
const { t } = useI18n()
const store = useNodeStore()
const mode = computed(() => (route.query.mode === 'edit' ? 'edit' : 'new'))
const maps = ref<MapInfo[]>([])
const navmeshFiles = ref<string[]>([])
const pipelineFiles = ref<PipelineFile[]>([])
const active = ref<EditableNode | null>(null)
const cleanState = ref('')
const selectedPoint = ref(-1)
const selectedMapId = ref('')
const selectedLayerFile = ref('')
const newDialog = ref(false)
const exportDialog = ref(false)
const exportTab = ref<ExportTab>('params')
const sidebarTab = ref<'browser' | 'properties'>('browser')
const recording = ref(false)
const lastRecordPosition = ref<Point | null>(null)
const locating = ref(false)
const saving = ref(false)
const refreshingPipeline = ref(true)
const requestError = ref('')
const pipelineSearch = ref('')
const guideExpanded = ref(readLocalFlag(GUIDE_EXPANDED_KEY, true))
const tourOpen = ref(false)
const newNodeTourOpen = ref(false)
const exportTourOpen = ref(false)
const recordTourSeen = ref(readLocalFlag(TOUR_RECORD_KEY))
const newNodeTourSeen = ref(readLocalFlag(TOUR_NEW_NODE_KEY))
const exportTourSeen = ref(readLocalFlag(TOUR_EXPORT_KEY))
const recordBtnRef = ref<ButtonInstance>()
const captureBtnRef = ref<ButtonInstance>()
const guidePanelRef = ref<HTMLElement>()
const newNameFieldRef = ref<FormItemInstance>()
const newTypeFieldRef = ref<FormItemInstance>()
const newMapFieldRef = ref<FormItemInstance>()
const exportTabsHostRef = ref<HTMLElement>()
const pipelineTreeRef = ref<TreeInstance>()
const mapCanvasRef = ref<MapCanvasExpose>()
const newForm = reactive({ name: '', nodeType: 'MapTrackerMove' as NodeType, mapId: '' })
const pipelineSkeletonRows = [
    { width: '44%', indent: 0 },
    { width: '68%', indent: 12 },
    { width: '57%', indent: 24 },
    { width: '72%', indent: 24 },
    { width: '51%', indent: 12 },
    { width: '64%', indent: 24 },
]
let dragIndex = -1
let dragMoved = false
let areaStart: Point | null = null
let recordingGeneration = 0
let recordingTimer: number | undefined

const selectedMap = computed(() => maps.value.find((map) => map.id === selectedMapId.value))
const tierOptions = computed(() => mapTierOptions(selectedMap.value, t('canvas.baseTier')))
const imageUrl = computed(() => (selectedLayerFile.value ? api.mapImage(selectedLayerFile.value) : ''))
const isMove = computed(() => active.value?.node_type === 'MapTrackerMove')
const isGoal = computed(() => active.value?.node_type === 'MapTrackerGoal')
const usesPath = computed(() => isMove.value || isGoal.value)
const isDirty = computed(
    () => Boolean(active.value?.source) && serializeNode(active.value!) !== cleanState.value,
)
const { confirmDiscard } = useUnsavedChangesGuard(isDirty)
const {
    history,
    future,
    snapshot,
    undo,
    redo,
    clear: clearHistory,
    discardSnapshot,
} = useUndoHistory(captureSnapshot, restoreSnapshot, () => recording.value)
const isValid = computed(() => {
    if (!active.value?.name.trim()) return false
    if (isMove.value) return active.value.path.length > 0
    if (isGoal.value) return active.value.path.length === 1
    return Boolean(active.value.target?.[2] && active.value.target?.[3])
})
const selectedPointValue = computed(() => active.value?.path[selectedPoint.value])
const navmeshMapIds = computed(
    () =>
        new Set(
            navmeshFiles.value.map((file) => {
                const name = file.split('/').pop() || file
                return name.replace(/\.mtnm$/i, '')
            }),
        ),
)
const exportOptions = computed(() => [
    { label: t('node.exportParams'), value: 'params' as const },
    { label: t('node.exportNode'), value: 'node' as const },
    { label: t('node.exportCoordinates'), value: 'coordinates' as const },
])
const pipelineTree = computed(() => buildPipelineTree(pipelineFiles.value))
const activePipelineNodeKey = computed(() => (active.value?.source ? active.value.id : undefined))

function compactTreeLabel(label: string, parentLabel?: string) {
    const parentStem = parentLabel?.replace(/\.[^.]+$/, '')
    return parentStem && label.startsWith(parentStem) ? `...${label.slice(parentStem.length)}` : label
}

function buildPipelineTree(files: PipelineFile[]) {
    const root: PipelineTreeItem[] = []
    const directories = new Map<string, PipelineTreeItem>()

    for (const file of files) {
        const parts = file.path.split('/')
        const fileName = parts.pop()!
        let directoryPath = ''
        let children = root
        let parentLabel: string | undefined

        for (const part of parts) {
            directoryPath = directoryPath ? `${directoryPath}/${part}` : part
            let directory = directories.get(directoryPath)
            if (!directory) {
                directory = {
                    id: `directory:${directoryPath}`,
                    label: part,
                    displayLabel: compactTreeLabel(part, parentLabel),
                    kind: 'directory',
                    children: [],
                }
                directories.set(directoryPath, directory)
                children.push(directory)
            }
            children = directory.children!
            parentLabel = part
        }

        children.push({
            id: `file:${file.path}`,
            label: fileName,
            displayLabel: compactTreeLabel(fileName, parentLabel),
            kind: 'file',
            children: file.nodes.map((node) => ({
                id: `pipeline:${file.path}:${node.node_name}`,
                label: node.node_name,
                displayLabel: compactTreeLabel(node.node_name, fileName),
                kind: 'node',
                file,
                node,
            })),
        })
    }

    return root
}

function round1(value: number) {
    return Math.round(value * 10) / 10
}

function serializeNode(node: EditableNode) {
    return JSON.stringify({ name: node.name, map_name: node.map_name, path: node.path, target: node.target })
}

function markClean() {
    if (active.value) cleanState.value = serializeNode(active.value)
}

function markNodeClean(node: EditableNode, savedState: string) {
    if (active.value?.id === node.id) cleanState.value = savedState
}

function captureSnapshot(): NodeSnapshot | undefined {
    if (!active.value) return
    return {
        path: active.value.path.map((point) => [...point]),
        target: active.value.target && [...active.value.target],
    }
}

function restoreSnapshot(state: NodeSnapshot) {
    if (!active.value) return
    active.value.path = state.path.map((point) => [...point])
    active.value.target = state.target && [...state.target]
    selectedPoint.value = -1
}

function pointAt(event: MapPointerInfo) {
    if (!active.value) return -1
    return active.value.path.findIndex(
        ([
            x,
            y,
        ]) => {
            const [
                screenX,
                screenY,
            ] = mapToScreen(event.viewport, x, y)
            return Math.hypot(screenX - event.screenX, screenY - event.screenY) <= 10
        },
    )
}

const pathLayer = {
    id: 'path',
    zIndex: 20,
    draw(context, view) {
        const points = active.value?.path || []
        context.save()
        context.lineCap = 'butt'
        context.strokeStyle = '#e6a23c'
        context.lineWidth = Math.max(1.3, Math.sqrt(view.zoom) * 1.3)
        context.beginPath()
        points.forEach(
            (
                [
                    x,
                    y,
                ],
                index,
            ) => {
                const [
                    screenX,
                    screenY,
                ] = mapToScreen(view, x, y)
                if (index) context.lineTo(screenX, screenY)
                else context.moveTo(screenX, screenY)
            },
        )
        context.stroke()
        const size = Math.max(3, Math.sqrt(view.zoom) * 3)
        points.forEach(
            (
                [
                    x,
                    y,
                ],
                index,
            ) => {
                const [
                    screenX,
                    screenY,
                ] = mapToScreen(view, x, y)
                context.fillStyle = index === selectedPoint.value ? '#f56c6c' : '#f2f3f5'
                context.strokeStyle = '#303133'
                context.lineWidth = 1.5
                context.beginPath()
                context.arc(screenX, screenY, size, 0, Math.PI * 2)
                context.fill()
                context.stroke()
                if (view.zoom >= 1 || index === 0 || index === points.length - 1) {
                    context.fillStyle = '#ffffff'
                    context.font = '11px sans-serif'
                    context.shadowColor = 'rgba(0, 0, 0, 0.75)'
                    context.shadowBlur = 3
                    context.shadowOffsetX = 0
                    context.shadowOffsetY = 1
                    context.fillText(String(index), screenX + 6, screenY - 6)
                    context.shadowColor = 'transparent'
                    context.shadowBlur = 0
                    context.shadowOffsetX = 0
                    context.shadowOffsetY = 0
                }
            },
        )
        context.restore()
    },
} satisfies MapLayer

const rectLayer = {
    id: 'assert-rect',
    zIndex: 20,
    draw(context, view) {
        if (!active.value?.target) return
        const [
            x,
            y,
            width,
            height,
        ] = active.value.target
        const [
            screenX,
            screenY,
        ] = mapToScreen(view, x, y)
        context.save()
        context.fillStyle = 'rgb(64 158 255 / 20%)'
        context.strokeStyle = '#409eff'
        context.lineWidth = 2
        context.fillRect(screenX, screenY, width * view.zoom, height * view.zoom)
        context.strokeRect(screenX, screenY, width * view.zoom, height * view.zoom)
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
const layers = computed<MapLayer[]>(() => [
    ...(usesPath.value ? [pathLayer] : [rectLayer]),
    recordCrosshairLayer,
])

function beginPointer(event: MapPointerInfo) {
    if (!active.value || recording.value || event.button !== 0) return
    if (usesPath.value) {
        dragIndex = pointAt(event)
        dragMoved = false
        selectedPoint.value = dragIndex
        if (dragIndex >= 0) sidebarTab.value = 'properties'
    } else {
        snapshot()
        areaStart = [
            round1(event.x),
            round1(event.y),
        ]
        active.value.target = [
            areaStart[0],
            areaStart[1],
            0,
            0,
        ]
    }
}

function movePointer(event: MapPointerInfo) {
    if (!active.value || recording.value) return
    if (usesPath.value && dragIndex >= 0 && event.buttons === 1) {
        if (!dragMoved) snapshot()
        dragMoved = true
        active.value.path[dragIndex] = [
            round1(event.x),
            round1(event.y),
        ]
    } else if (!usesPath.value && areaStart && event.buttons === 1) {
        active.value.target = [
            round1(Math.min(areaStart[0], event.x)),
            round1(Math.min(areaStart[1], event.y)),
            round1(Math.abs(event.x - areaStart[0])),
            round1(Math.abs(event.y - areaStart[1])),
        ]
    }
}

function endPointer(event: MapPointerInfo) {
    if (!active.value || recording.value) return
    if (!usesPath.value) {
        if (active.value.target && (!active.value.target[2] || !active.value.target[3])) {
            discardSnapshot()
            active.value.target = null
        } else if (active.value.target) {
            sidebarTab.value = 'properties'
        }
        areaStart = null
        return
    }
    if (dragIndex >= 0) {
        if (dragMoved) sidebarTab.value = 'properties'
        dragIndex = -1
        return
    }
    const point: Point = [
        round1(event.x),
        round1(event.y),
    ]
    snapshot()
    if (isGoal.value) {
        active.value.path = [point]
        selectedPoint.value = 0
        sidebarTab.value = 'properties'
        return
    }
    let insertAt = -1
    for (let index = 1; index < active.value.path.length; index += 1) {
        if (
            distanceToSegment(
                point[0],
                point[1],
                active.value.path[index - 1][0],
                active.value.path[index - 1][1],
                active.value.path[index][0],
                active.value.path[index][1],
            ) <
            10 / event.viewport.zoom
        ) {
            insertAt = index
            break
        }
    }
    if (insertAt >= 0) active.value.path.splice(insertAt, 0, point)
    else active.value.path.push(point)
    selectedPoint.value = insertAt >= 0 ? insertAt : active.value.path.length - 1
    sidebarTab.value = 'properties'
}

function deleteSelected() {
    if (!active.value || recording.value) return
    if (usesPath.value && selectedPoint.value < 0) return
    snapshot()
    if (usesPath.value) {
        active.value.path.splice(selectedPoint.value, 1)
        selectedPoint.value = Math.min(selectedPoint.value, active.value.path.length - 1)
    } else active.value.target = null
}

function selectPrevPoint() {
    if (!active.value || !isMove.value || !active.value.path.length) return
    selectedPoint.value =
        selectedPoint.value < 0 ? active.value.path.length - 1 : Math.max(0, selectedPoint.value - 1)
    sidebarTab.value = 'properties'
}

function selectNextPoint() {
    if (!active.value || !isMove.value || !active.value.path.length) return
    selectedPoint.value =
        selectedPoint.value < 0 ? 0 : Math.min(active.value.path.length - 1, selectedPoint.value + 1)
    sidebarTab.value = 'properties'
}

function updateSelectedPoint(axis: 0 | 1, value: number | undefined) {
    if (!selectedPointValue.value || value === undefined) return
    snapshot()
    selectedPointValue.value[axis] = round1(value)
}

function updateTarget(index: 0 | 1 | 2 | 3, value: number | undefined) {
    if (!active.value?.target || value === undefined) return
    snapshot()
    active.value.target[index] = round1(value)
}

async function copySelected() {
    if (!selectedPointValue.value) return
    await navigator.clipboard.writeText(JSON.stringify(selectedPointValue.value))
    ElMessage.success(t('common.copied'))
}

async function activateNode(node: EditableNode) {
    if (saving.value || active.value?.id === node.id || !(await confirmDiscard())) return
    stopRecording()
    active.value = {
        ...node,
        path: node.path.map((point) => [...point]),
        target: node.target && [...node.target],
        source: node.source && { ...node.source },
    }
    const map = findMapByName(maps.value, node.map_name)
    selectedMapId.value = map?.id || ''
    selectedLayerFile.value =
        findLayerByName(map, node.map_name)?.file_name || map?.layers[0]?.file_name || ''
    selectedPoint.value = -1
    clearHistory()
    await nextTick()
    markClean()
    adaptView()
}

function adaptView() {
    const canvas = mapCanvasRef.value
    const node = active.value
    if (!canvas || !node) return
    if (node.node_type === 'MapTrackerMove' || node.node_type === 'MapTrackerGoal') {
        if (node.path.length) {
            canvas.fitTo(node.path, { padding: 0.3, minZoom: 1, maxZoom: 5 })
            return
        }
        canvas.fit()
        return
    }
    const target = node.target
    if (target && target[2] > 0 && target[3] > 0) {
        canvas.fitTo(
            [
                [target[0], target[1]],
                [target[0] + target[2], target[1] + target[3]],
            ],
            { padding: 0.2, minZoom: 1, maxZoom: 5 },
        )
        return
    }
    canvas.fit()
}

function activatePipeline(file: PipelineFile, node: PipelineFile['nodes'][number]) {
    const now = new Date().toISOString()
    return activateNode({
        id: `pipeline:${file.path}:${node.node_name}`,
        name: node.node_name,
        node_type: node.node_type,
        map_name: node.map_name,
        path: node.path || [],
        target: node.target,
        is_new_structure: node.is_new_structure,
        created_at: now,
        updated_at: now,
        source: { file_path: file.path, revision: file.revision },
    })
}

function openNewDialog() {
    if (saving.value) return
    newForm.name = 'NewNode'
    newForm.nodeType = 'MapTrackerMove'
    newForm.mapId = ''
    newDialog.value = true
}

function mapHasNavmesh(map: MapInfo) {
    if (navmeshMapIds.value.has(map.id)) return true
    return map.layers.some((layer) => navmeshMapIds.value.has(stripMapExtension(layer.map_name)))
}

function confirmGoalWithoutNavmesh(): Promise<'cancel' | 'move' | 'goal'> {
    return new Promise((resolve) => {
        let settled = false
        const finish = (action: 'cancel' | 'move' | 'goal') => {
            if (settled) return
            settled = true
            ElMessageBox.close()
            resolve(action)
        }
        void ElMessageBox({
            title: t('node.navmeshMissingTitle'),
            type: 'warning',
            showConfirmButton: false,
            showCancelButton: false,
            showClose: true,
            closeOnClickModal: false,
            closeOnPressEscape: true,
            message: () =>
                h('div', { class: 'space-y-4' }, [
                    h('p', { class: 'm-0 leading-6' }, t('node.navmeshMissingMessage')),
                    h('div', { class: 'flex flex-wrap justify-end gap-2' }, [
                        h(ElButton, { onClick: () => finish('cancel') }, () => t('common.cancel')),
                        h(ElButton, { onClick: () => finish('move') }, () => t('node.switchToMove')),
                        h(
                            ElButton,
                            { type: 'warning', onClick: () => finish('goal') },
                            () => t('node.createGoalAnyway'),
                        ),
                    ]),
                ]),
            callback: () => finish('cancel'),
        })
    })
}

async function createNode() {
    if (saving.value || !newForm.name.trim() || !newForm.mapId) return
    const map = maps.value.find((item) => item.id === newForm.mapId)
    const layer = map?.layers[0]
    if (!map || !layer) return
    let nodeType = newForm.nodeType
    if (nodeType === 'MapTrackerGoal' && !mapHasNavmesh(map)) {
        const decision = await confirmGoalWithoutNavmesh()
        if (decision === 'cancel') return
        if (decision === 'move') nodeType = 'MapTrackerMove'
    }
    if (active.value && !(await confirmDiscard())) return
    stopRecording()
    const now = new Date().toISOString()
    active.value = {
        id: crypto.randomUUID(),
        name: newForm.name.trim(),
        node_type: nodeType,
        map_name: layer.map_name,
        path: [],
        target: null,
        created_at: now,
        updated_at: now,
    }
    store.save(active.value)
    selectedMapId.value = map.id
    selectedLayerFile.value = layer.file_name
    clearHistory()
    cleanState.value = ''
    newDialog.value = false
    await nextTick()
    adaptView()
}

async function savePipeline() {
    const node = active.value
    if (saving.value) return
    if (!node?.source || !isValid.value) return ElMessage.warning(t('node.incomplete'))
    const savedState = serializeNode(node)
    const mapName = stripMapExtension(node.map_name)
    const path = node.path.map<Point>((point) => [...point])
    const target: Rect | null = node.target && [...node.target]
    saving.value = true
    try {
        const result = await api.savePipelineNode({
            file_path: node.source.file_path,
            revision: node.source.revision,
            node_name: node.name,
            node_type: node.node_type,
            map_name: mapName,
            path,
            target,
        })
        node.source.revision = result.revision
        const pipelineFile = pipelineFiles.value.find((file) => file.path === node.source?.file_path)
        if (pipelineFile) {
            pipelineFile.revision = result.revision
            const pipelineNode = pipelineFile.nodes.find((item) => item.node_name === node.name)
            if (pipelineNode) {
                pipelineNode.map_name = mapName
                pipelineNode.path = path
                pipelineNode.target = target
            }
        }
        markNodeClean(node, savedState)
        ElMessage.success(t('common.saved'))
    } catch (error) {
        ElMessage.error(errorMessage(error))
    } finally {
        saving.value = false
    }
}

function exportValues(): Record<ExportTab, string> {
    if (!active.value) return { params: '', node: '', coordinates: '' }
    const node = active.value
    const mapName = stripMapExtension(node.map_name)
    const param = isMove.value
        ? { map_name: mapName, path: node.path }
        : isGoal.value
          ? { map_name: mapName, target: node.path[0] }
          : { expected: [{ map_name: mapName, target: node.target }] }
    const body =
        isMove.value || isGoal.value
            ? node.is_new_structure
                ? { action: { custom_action: node.node_type, custom_action_param: param } }
                : { action: 'Custom', custom_action: node.node_type, custom_action_param: param }
            : {
                  recognition: 'Custom',
                  custom_recognition: node.node_type,
                  custom_recognition_param: param,
                  action: 'DoNothing',
              }
    return {
        params: JSON.stringify(param, null, 4),
        node: JSON.stringify({ [node.name]: body }, null, 4),
        coordinates: JSON.stringify(usesPath.value ? (isGoal.value ? node.path[0] : node.path) : node.target),
    }
}
const currentExport = computed(() => exportValues()[exportTab.value])

function openExport() {
    if (!isValid.value) return ElMessage.warning(t('node.incomplete'))
    exportDialog.value = true
}

async function copyExport() {
    await navigator.clipboard.writeText(currentExport.value)
    ElMessage.success(t('common.copied'))
}

function canSimplify(previous: Point, middle: Point, next: Point) {
    const ax = middle[0] - previous[0]
    const ay = middle[1] - previous[1]
    const bx = next[0] - middle[0]
    const by = next[1] - middle[1]
    const distance = Math.hypot(bx, by)
    if (distance < 0.100001) return true
    const sin = Math.abs(ax * by - ay * bx) / (Math.hypot(ax, ay) * distance + 0.000001)
    return sin * (distance + 1) < 1.1
}

function appendLocation(result: { x: number; y: number; map_name: string; loc_conf: number }) {
    if (!active.value) return
    const point: Point = [
        round1(result.x),
        round1(result.y),
    ]
    if (isGoal.value) {
        active.value.path = [point]
        selectedPoint.value = 0
    } else {
        const path = active.value.path
        if (path.length >= 2 && canSimplify(path[path.length - 2], path[path.length - 1], point)) path.pop()
        if (!path.length || path.at(-1)![0] !== point[0] || path.at(-1)![1] !== point[1]) path.push(point)
        selectedPoint.value = path.length - 1
    }
    const layer = findLayerByName(findMapByName(maps.value, result.map_name), result.map_name)
    if (layer) selectedLayerFile.value = layer.file_name
    mapCanvasRef.value?.maybeCenterTo(point[0], point[1])
}

async function recordStep(generation: number, nodeId: string) {
    if (!active.value || generation !== recordingGeneration || active.value.id !== nodeId) return
    try {
        const result = await api.infer(active.value.map_name)
        if (generation !== recordingGeneration || active.value?.id !== nodeId) return
        requestError.value = ''
        lastRecordPosition.value = [result.x, result.y]
        appendLocation(result)
    } catch (error) {
        if (generation === recordingGeneration) requestError.value = errorMessage(error)
    }
    if (recording.value && generation === recordingGeneration) {
        recordingTimer = window.setTimeout(() => recordStep(generation, nodeId), 300)
    }
}

async function toggleContinuousRecord() {
    if (!active.value || !isMove.value || locating.value) return
    if (recording.value) return stopRecording()
    snapshot()
    active.value.path = []
    selectedPoint.value = -1
    lastRecordPosition.value = null
    requestError.value = ''
    const generation = ++recordingGeneration
    const nodeId = active.value.id
    recording.value = true
    await recordStep(generation, nodeId)
}

async function captureLocation() {
    if (!active.value || !usesPath.value || recording.value || locating.value) return
    locating.value = true
    requestError.value = ''
    snapshot()
    try {
        const result = await api.infer(active.value.map_name)
        if (active.value?.id) appendLocation(result)
    } catch (error) {
        requestError.value = errorMessage(error)
    } finally {
        locating.value = false
    }
}

function stopRecording() {
    recording.value = false
    lastRecordPosition.value = null
    recordingGeneration += 1
    window.clearTimeout(recordingTimer)
}

async function removeLocal(id: string) {
    if (saving.value || recording.value) return
    try {
        await ElMessageBox.confirm(t('node.deleteConfirm'), t('common.delete'), {
            type: 'warning',
            confirmButtonText: t('common.confirm'),
            cancelButtonText: t('common.cancel'),
        })
    } catch {
        return
    }
    store.remove(id)
    if (active.value?.id === id) {
        active.value = null
        cleanState.value = ''
    }
}

async function clearLocalNodes() {
    if (saving.value || recording.value || !store.nodes.length) return
    try {
        await ElMessageBox.confirm(t('node.clearAllConfirm'), t('node.clearAll'), {
            type: 'warning',
            confirmButtonText: t('common.confirm'),
            cancelButtonText: t('common.cancel'),
        })
    } catch {
        return
    }
    const closesActiveNode = Boolean(active.value && !active.value.source)
    store.clear()
    if (closesActiveNode) {
        active.value = null
        cleanState.value = ''
        selectedPoint.value = -1
        clearHistory()
    }
}

async function refreshPipeline() {
    if (refreshingPipeline.value) return
    refreshingPipeline.value = true
    try {
        pipelineFiles.value = await api.pipelineFiles()
    } catch (error) {
        ElMessage.error(errorMessage(error))
    } finally {
        refreshingPipeline.value = false
    }
}

function startTour() {
    writeLocalFlag(TOUR_RECORD_KEY, true)
    recordTourSeen.value = true
    guideExpanded.value = true
    tourOpen.value = true
}

function startNewNodeTour() {
    writeLocalFlag(TOUR_NEW_NODE_KEY, true)
    newNodeTourSeen.value = true
    newNodeTourOpen.value = true
}

function startExportTour() {
    writeLocalFlag(TOUR_EXPORT_KEY, true)
    exportTourSeen.value = true
    exportTourOpen.value = true
}

function exportTabTarget(name: ExportTab) {
    return () => exportTabsHostRef.value?.querySelector<HTMLElement>(`#tab-${name}`) ?? null
}

function onTourChange(current: number) {
    if (current === 2) guideExpanded.value = true
}

function onTreeClick(data: PipelineTreeItem) {
    if (data.file && data.node) activatePipeline(data.file, data.node)
}

function filterPipelineNode(value: string, data: TreeNodeData) {
    return !value.trim() || String(data.label).toLocaleLowerCase().includes(value.trim().toLocaleLowerCase())
}

function onKeydown(event: KeyboardEvent) {
    if (isInteractiveTarget(event.target)) return
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'z') {
        event.preventDefault()
        event.shiftKey ? redo() : undo()
    } else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'y') {
        event.preventDefault()
        redo()
    } else if (!event.ctrlKey && !event.metaKey && !event.altKey && event.key === 'Delete') {
        event.preventDefault()
        deleteSelected()
    } else if (!event.ctrlKey && !event.metaKey && !event.altKey && (event.key === '-' || event.key === 'Subtract')) {
        event.preventDefault()
        selectPrevPoint()
    } else if (!event.ctrlKey && !event.metaKey && !event.altKey && (event.key === '=' || event.key === 'Equal')) {
        event.preventDefault()
        selectNextPoint()
    }
}

watch(selectedLayerFile, (file) => {
    const layer = selectedMap.value?.layers.find((item) => item.file_name === file)
    if (active.value && layer && !(active.value.source && usesPath.value)) active.value.map_name = layer.map_name
})
watch(
    active,
    (node) => {
        if (node && !node.source) store.save(node)
    },
    { deep: true },
)
watch(
    [pipelineTree, activePipelineNodeKey],
    async ([, key]) => {
        await nextTick()
        pipelineTreeRef.value?.filter(pipelineSearch.value)
        pipelineTreeRef.value?.setCurrentKey(key, true)
    },
    { flush: 'post' },
)
watch(mode, (value) => {
    if (value === 'new') openNewDialog()
    else newDialog.value = false
})
watch(newDialog, (open) => {
    if (!open) newNodeTourOpen.value = false
})
watch(exportDialog, (open) => {
    if (!open) exportTourOpen.value = false
})
watch(guideExpanded, (expanded) => writeLocalFlag(GUIDE_EXPANDED_KEY, expanded))
async function loadData() {
    const [
        mapResult,
        pipelineResult,
        navmeshResult,
    ] = await Promise.allSettled([
        api.maps(),
        api.pipelineFiles(),
        api.navmeshes(),
    ])
    if (mapResult.status === 'fulfilled') maps.value = mapResult.value
    else ElMessage.error(errorMessage(mapResult.reason))
    if (pipelineResult.status === 'fulfilled') pipelineFiles.value = pipelineResult.value
    else ElMessage.error(errorMessage(pipelineResult.reason))
    if (navmeshResult.status === 'fulfilled') navmeshFiles.value = navmeshResult.value
    else ElMessage.error(errorMessage(navmeshResult.reason))
    refreshingPipeline.value = false
    if (mode.value === 'new') openNewDialog()
}

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
            class="max-h-60 w-full shrink-0 overflow-auto border-b border-(--el-border-color) md:max-h-none md:w-72 md:border-r md:border-b-0">
            <el-tabs v-model="sidebarTab" class="px-3">
                <el-tab-pane :label="t('node.browser')" name="browser">
                    <section>
                        <div class="flex items-center justify-between">
                            <h2 class="text-base font-medium">{{ t('node.newNode') }}</h2>
                            <el-button
                                link
                                :icon="Brush"
                                :disabled="!store.nodes.length || recording"
                                @click="clearLocalNodes">
                                {{ t('node.clearAll') }}
                            </el-button>
                        </div>
                        <el-menu v-if="store.nodes.length" :default-active="active?.id" class="mt-2 border-0!">
                            <el-menu-item
                                v-for="node in store.nodes"
                                :key="node.id"
                                :index="node.id"
                                @click="activateNode(node)">
                                <span class="min-w-0 flex-1 truncate">{{ node.name }}</span>
                                <el-button
                                    link
                                    type="danger"
                                    :icon="Delete"
                                    :disabled="recording"
                                    :title="t('common.delete')"
                                    @click.stop="removeLocal(node.id)" />
                            </el-menu-item>
                        </el-menu>
                        <el-button class="mt-2 w-full" :icon="Plus" @click="openNewDialog">{{
                            t('common.create')
                        }}</el-button>
                    </section>

                    <el-divider class="my-4!" />

                    <section>
                        <div class="flex items-center justify-between">
                            <h2 class="text-base font-medium">{{ t('node.existingNodes') }}</h2>
                            <el-button link :icon="Refresh" :loading="refreshingPipeline" @click="refreshPipeline">
                                {{ t('common.refresh') }}
                            </el-button>
                        </div>
                        <el-input
                            v-model="pipelineSearch"
                            class="mt-2"
                            clearable
                            :prefix-icon="Search"
                            :placeholder="t('node.searchExistingNodes')"
                            @input="pipelineTreeRef?.filter($event)" />
                        <el-skeleton v-if="refreshingPipeline" animated class="mt-3 px-2">
                            <template #template>
                                <div class="space-y-3">
                                    <div
                                        v-for="(row, index) in pipelineSkeletonRows"
                                        :key="index"
                                        class="flex h-5 items-center"
                                        :style="{ paddingLeft: `${row.indent}px` }">
                                        <el-skeleton-item variant="text" :style="{ width: row.width }" />
                                    </div>
                                </div>
                            </template>
                        </el-skeleton>
                        <el-empty
                            v-else-if="!pipelineTree.length"
                            class="py-3!"
                            :image-size="0"
                            :description="t('node.noExistingNodes')" />
                        <el-tree
                            v-else
                            ref="pipelineTreeRef"
                            class="mt-2"
                            :data="pipelineTree"
                            node-key="id"
                            :indent="12"
                            highlight-current
                            :current-node-key="activePipelineNodeKey"
                            :filter-node-method="filterPipelineNode"
                            @node-click="onTreeClick">
                            <template #default="{ data }">
                                <el-icon class="mr-1 shrink-0">
                                    <Folder v-if="data.kind === 'directory'" />
                                    <Document v-else-if="data.kind === 'file'" />
                                    <Location v-else />
                                </el-icon>
                                <el-tooltip
                                    :content="data.label"
                                    placement="right"
                                    :disabled="data.displayLabel === data.label"
                                    :show-after="400">
                                    <span class="truncate">{{ data.displayLabel }}</span>
                                </el-tooltip>
                            </template>
                        </el-tree>
                    </section>
                </el-tab-pane>
                <el-tab-pane :label="t('common.properties')" name="properties">
                    <el-empty v-if="!active" :image-size="0" :description="t('node.noOpenNode')" />
                    <el-form v-else label-position="top">
                        <el-form-item :label="t('common.name')"
                            ><el-input v-model="active.name" :disabled="Boolean(active.source)"
                        /></el-form-item>
                        <el-form-item :label="t('common.type')"
                            ><el-input :model-value="active.node_type" disabled
                        /></el-form-item>
                        <template v-if="selectedPointValue">
                            <el-divider>{{ t('node.pathPoint', { index: selectedPoint }) }}</el-divider>
                            <div class="grid grid-cols-2 gap-x-3">
                                <el-form-item :label="t('node.xCoord')"
                                    ><el-input-number
                                        class="w-full"
                                        :model-value="selectedPointValue[0]"
                                        :precision="1"
                                        controls-position="right"
                                        @change="updateSelectedPoint(0, $event)"
                                /></el-form-item>
                                <el-form-item :label="t('node.yCoord')"
                                    ><el-input-number
                                        class="w-full"
                                        :model-value="selectedPointValue[1]"
                                        :precision="1"
                                        controls-position="right"
                                        @change="updateSelectedPoint(1, $event)"
                                /></el-form-item>
                            </div>
                            <div class="grid grid-cols-2 gap-x-3">
                                <el-button class="w-full" :icon="CopyDocument" @click="copySelected">{{
                                    t('node.copyCoordinates')
                                }}</el-button>
                                <el-button
                                    class="ml-0! w-full"
                                    type="danger"
                                    plain
                                    :icon="Delete"
                                    :disabled="recording"
                                    @click="deleteSelected">
                                    {{ t('node.deletePoint') }}
                                </el-button>
                            </div>
                            <div v-if="isMove" class="mt-3 grid grid-cols-2 gap-x-3">
                                <el-button
                                    class="w-full"
                                    :icon="ArrowLeft"
                                    :disabled="selectedPoint <= 0"
                                    @click="selectPrevPoint">
                                    {{ t('node.prevPoint') }}
                                </el-button>
                                <el-button
                                    class="ml-0! w-full"
                                    :disabled="selectedPoint < 0 || selectedPoint >= active.path.length - 1"
                                    @click="selectNextPoint">
                                    {{ t('node.nextPoint') }}
                                    <el-icon class="el-icon--right"><ArrowRight /></el-icon>
                                </el-button>
                            </div>
                        </template>
                        <template v-else-if="active.target">
                            <el-divider>{{ t('common.region') }}</el-divider>
                            <div class="grid grid-cols-2 gap-x-3">
                                <el-form-item
                                    v-for="(label, index) in [
                                        t('node.xCoord'),
                                        t('node.yCoord'),
                                        t('node.width'),
                                        t('node.height'),
                                    ]"
                                    :key="index"
                                    :label="label">
                                    <el-input-number
                                        class="w-full"
                                        :model-value="active.target[index as 0 | 1 | 2 | 3]"
                                        :precision="1"
                                        controls-position="right"
                                        @change="updateTarget(index as 0 | 1 | 2 | 3, $event)" />
                                </el-form-item>
                            </div>
                        </template>
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
            <template v-if="active">
                <div class="border-b border-(--el-border-color)">
                    <div class="grid min-h-12 grid-cols-[1fr_minmax(0,40%)_1fr] items-center gap-2 px-3 py-2">
                        <el-button-group class="justify-self-start">
                            <el-button
                                :icon="RefreshLeft"
                                :disabled="!history.length || recording"
                                :title="t('common.undo')"
                                @click="undo">
                                <span class="hidden sm:inline">{{ t('common.undo') }}</span>
                            </el-button>
                            <el-button
                                :icon="RefreshRight"
                                :disabled="!future.length || recording"
                                :title="t('common.redo')"
                                @click="redo">
                                <span class="hidden sm:inline">{{ t('common.redo') }}</span>
                            </el-button>
                        </el-button-group>
                        <span class="truncate text-center font-medium" :title="active.name">{{ active.name }}</span>
                        <div class="flex justify-self-end gap-1">
                            <el-button
                                :type="active.source ? 'default' : 'primary'"
                                :icon="Download"
                                :title="t('common.exportAs')"
                                @click="openExport">
                                <span class="hidden sm:inline">{{ t('common.exportAs') }}</span>
                            </el-button>
                            <el-button
                                v-if="active.source"
                                type="primary"
                                :icon="Save"
                                :loading="saving"
                                :disabled="!isDirty || recording"
                                :title="t('common.save')"
                                @click="savePipeline">
                                <span class="hidden sm:inline">{{ t('common.save') }}</span>
                            </el-button>
                        </div>
                    </div>
                    <div
                        v-if="usesPath"
                        class="flex min-h-12 flex-wrap items-center gap-1 border-t border-(--el-border-color) px-3 py-2">
                        <el-button
                            v-if="isMove"
                            ref="recordBtnRef"
                            :type="recording ? 'danger' : 'default'"
                            :icon="recording ? VideoPause : VideoCamera"
                            :disabled="locating"
                            @click="toggleContinuousRecord">
                            {{ recording ? t('node.stopRecording') : t('node.startContinuousRecording') }}
                        </el-button>
                        <el-button
                            ref="captureBtnRef"
                            :icon="Location"
                            :loading="locating"
                            :disabled="recording"
                            @click="captureLocation">
                            {{ t('node.captureLocation') }}
                        </el-button>
                        <el-button
                            v-if="isMove"
                            class="ml-auto"
                            :type="recordTourSeen ? 'default' : 'warning'"
                            :icon="BookOpen"
                            @click="startTour">
                            {{ t('node.tour.watch') }}
                        </el-button>
                    </div>
                </div>
                <div class="min-h-0 flex-1">
                    <MapCanvas
                        ref="mapCanvasRef"
                        v-model:tier-value="selectedLayerFile"
                        :image-url="imageUrl"
                        :image-width="selectedMap?.width"
                        :image-height="selectedMap?.height"
                        :viewport-key="selectedMap?.id"
                        :layers="layers"
                        :tier-options="tierOptions"
                        :tier-disabled="recording"
                        @ready="adaptView"
                        @pointerdown="beginPointer"
                        @pointermove="movePointer"
                        @pointerup="endPointer">
                        <template #overlay>
                            <div
                                ref="guidePanelRef"
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
                                            <span>{{
                                                isMove
                                                    ? t('node.guide.addOrMovePoint')
                                                    : isGoal
                                                      ? t('node.guide.placeGoal')
                                                      : t('node.guide.createRegion')
                                            }}</span>
                                        </li>
                                        <template v-if="usesPath">
                                            <li class="flex items-start gap-2">
                                                <span class="shrink-0 rounded bg-white/15 px-1.5 py-0.5 font-mono"
                                                    >Delete</span
                                                >
                                                <span>{{
                                                    isGoal ? t('node.guide.deleteGoal') : t('node.guide.deletePoint')
                                                }}</span>
                                            </li>
                                            <li v-if="isMove" class="flex items-start gap-2">
                                                <span class="flex shrink-0 gap-1">
                                                    <span class="rounded bg-white/15 px-1.5 py-0.5 font-mono">-</span>
                                                    <span class="rounded bg-white/15 px-1.5 py-0.5 font-mono">=</span>
                                                </span>
                                                <span>{{ t('node.guide.prevNextPoint') }}</span>
                                            </li>
                                        </template>
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
                        </template>
                    </MapCanvas>
                </div>
            </template>
            <el-empty v-else class="h-full">
                <template #description>
                    <div class="space-y-1 text-center">
                        <p>{{ t('node.noOpenNode') }}</p>
                        <el-text type="info">{{ t('node.noOpenHint') }}</el-text>
                    </div>
                </template>
            </el-empty>
        </main>

        <el-tour
            v-model="tourOpen"
            :target-area-clickable="false"
            :content-style="{ maxWidth: '22rem' }"
            @change="onTourChange">
            <el-tour-step
                :target="recordBtnRef?.$el"
                :title="t('node.startContinuousRecording')"
                :description="t('node.tour.record')"
                placement="bottom" />
            <el-tour-step
                :target="captureBtnRef?.$el"
                :title="t('node.captureLocation')"
                :description="t('node.tour.capture')"
                placement="bottom" />
            <el-tour-step
                :target="guidePanelRef"
                :title="t('node.guide.title')"
                :description="t('node.tour.guide')"
                placement="left" />
        </el-tour>

        <el-dialog
            v-model="newDialog"
            :title="t('node.newNode')"
            width="94%"
            class="max-w-200"
            :close-on-click-modal="true"
            :close-on-press-escape="true"
            :show-close="true">
            <el-form label-position="top">
                <el-form-item ref="newNameFieldRef" :label="t('common.name')" required
                    ><el-input v-model="newForm.name" autofocus
                /></el-form-item>
                <el-form-item ref="newTypeFieldRef" :label="t('common.type')" required>
                    <el-radio-group v-model="newForm.nodeType">
                        <el-radio-button value="MapTrackerMove">Move</el-radio-button>
                        <el-radio-button value="MapTrackerGoal">Goal</el-radio-button>
                        <el-radio-button value="MapTrackerAssertLocation">AssertLocation</el-radio-button>
                    </el-radio-group>
                </el-form-item>
                <el-form-item ref="newMapFieldRef" :label="t('common.map')" required>
                    <MapPicker v-model="newForm.mapId" :maps="maps" />
                </el-form-item>
            </el-form>
            <template #footer>
                <div class="flex w-full items-center justify-between gap-2">
                    <el-button
                        :type="newNodeTourSeen ? 'default' : 'warning'"
                        :icon="BookOpen"
                        @click="startNewNodeTour"
                        >{{ t('node.tour.watch') }}</el-button
                    >
                    <el-button
                        type="primary"
                        :icon="Plus"
                        :disabled="!newForm.name.trim() || !newForm.mapId"
                        @click="createNode"
                        >{{ t('common.create') }}</el-button
                    >
                </div>
            </template>
        </el-dialog>

        <el-tour
            v-model="newNodeTourOpen"
            :z-index="4000"
            :target-area-clickable="false"
            :content-style="{ maxWidth: '22rem' }">
            <el-tour-step
                :target="newNameFieldRef?.$el"
                :title="t('common.name')"
                :description="t('node.tour.newName')"
                placement="bottom" />
            <el-tour-step
                :target="newTypeFieldRef?.$el"
                :title="t('common.type')"
                :description="t('node.tour.newType')"
                placement="bottom" />
            <el-tour-step
                :target="newMapFieldRef?.$el"
                :title="t('common.map')"
                :description="t('node.tour.newMap')"
                placement="bottom" />
        </el-tour>

        <el-dialog v-model="exportDialog" :title="t('common.exportAs')" width="94%" class="max-w-180">
            <div ref="exportTabsHostRef">
                <el-tabs v-model="exportTab">
                    <el-tab-pane
                        v-for="option in exportOptions"
                        :key="option.value"
                        :label="option.label"
                        :name="option.value" />
                </el-tabs>
            </div>
            <el-input :model-value="currentExport" type="textarea" :autosize="{ minRows: 12, maxRows: 22 }" readonly />
            <template #footer>
                <div class="flex w-full items-center justify-between gap-2">
                    <el-button
                        :type="exportTourSeen ? 'default' : 'warning'"
                        :icon="BookOpen"
                        @click="startExportTour"
                        >{{ t('node.tour.watch') }}</el-button
                    >
                    <el-button type="primary" :icon="CopyDocument" @click="copyExport">{{
                        t('common.copy')
                    }}</el-button>
                </div>
            </template>
        </el-dialog>

        <el-tour
            v-model="exportTourOpen"
            :z-index="4000"
            :target-area-clickable="false"
            :content-style="{ maxWidth: '22rem' }">
            <el-tour-step
                :target="exportTabTarget('params')"
                :title="t('node.exportParams')"
                :description="t('node.tour.exportParams')"
                placement="bottom" />
            <el-tour-step
                :target="exportTabTarget('node')"
                :title="t('node.exportNode')"
                :description="t('node.tour.exportNode')"
                placement="bottom" />
            <el-tour-step
                :target="exportTabTarget('coordinates')"
                :title="t('node.exportCoordinates')"
                :description="t('node.tour.exportCoordinates')"
                placement="bottom" />
        </el-tour>
    </div>
</template>
