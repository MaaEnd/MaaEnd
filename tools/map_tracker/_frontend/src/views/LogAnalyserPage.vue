<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UploadFile } from 'element-plus'
import {
    DArrowLeft,
    DArrowRight,
    FolderOpened,
    RefreshRight,
    UploadFilled,
    VideoPause,
    VideoPlay,
    Warning,
} from '@element-plus/icons-vue'
import MapCanvas from '@/components/MapCanvas.vue'
import { mapToScreen, type MapLayer } from '@/utils/canvas'
import { api } from '@/api'
import type { MapInfo, TrackPeriod } from '@/types'
import {
    errorMessage as toErrorMessage,
    findLayerByName,
    findMapByName,
    isInteractiveTarget,
    mapNameKey,
    mapTierOptions,
} from '@/utils/misc'

const { locale, t } = useI18n()
const maps = ref<MapInfo[]>([])
const periods = ref<TrackPeriod[]>([])
const selectedPeriodId = ref<number>()
const frame = ref(0)
const playing = ref(false)
const speed = ref(1)
const loading = ref(false)
const fileName = ref('')
const warnings = ref<Array<{ line: number; message: string }>>([])
const warningDrawer = ref(false)
const errorMessage = ref('')
const selectedLayerFile = ref('')
let timer: number | undefined

const selectedPeriod = computed(() => periods.value.find((period) => period.period_id === selectedPeriodId.value))
const currentPoint = computed(() => selectedPeriod.value?.points[frame.value])
const currentMap = computed(() => findMapByName(maps.value, currentPoint.value?.map_name || ''))
const inferredLayer = computed(
    () => findLayerByName(currentMap.value, currentPoint.value?.map_name || '') || currentMap.value?.layers[0],
)
const selectedLayer = computed(
    () => currentMap.value?.layers.find((layer) => layer.file_name === selectedLayerFile.value) || inferredLayer.value,
)
const tierOptions = computed(() => mapTierOptions(currentMap.value, t('canvas.baseTier')))
const imageUrl = computed(() => (selectedLayer.value ? api.mapImage(selectedLayer.value.file_name) : ''))
const visiblePoints = computed(() => {
    if (!selectedPeriod.value || !selectedLayer.value) return []
    const mapName = mapNameKey(selectedLayer.value.map_name)
    return selectedPeriod.value.points
        .map((point, index) => ({ point, index }))
        .filter(({ point, index }) => index <= frame.value && mapNameKey(point.map_name) === mapName)
})

watch(
    inferredLayer,
    (layer) => {
        selectedLayerFile.value = layer?.file_name || ''
    },
    { immediate: true },
)

const trajectoryLayer = {
    id: 'trajectory',
    zIndex: 20,
    draw(context, view) {
        if (!visiblePoints.value.length) return
        context.save()
        context.lineWidth = 1.5
        for (let index = 1; index < visiblePoints.value.length; index += 1) {
            const previous = visiblePoints.value[index - 1]
            const current = visiblePoints.value[index]
            const timeGap =
                previous.point.timestamp !== null && current.point.timestamp !== null
                    ? current.point.timestamp - previous.point.timestamp
                    : 0
            if (current.index !== previous.index + 1 || timeGap > 10) continue
            const [
                x1,
                y1,
            ] = mapToScreen(view, previous.point.x, previous.point.y)
            const [
                x2,
                y2,
            ] = mapToScreen(view, current.point.x, current.point.y)
            context.strokeStyle = current.point.loc_conf < 0.5 ? '#e6a23c' : '#f56c6c'
            context.beginPath()
            context.moveTo(x1, y1)
            context.lineTo(x2, y2)
            context.stroke()
        }
        visiblePoints.value.forEach(({ point }, index) => {
            const [
                x,
                y,
            ] = mapToScreen(view, point.x, point.y)
            const current = index === visiblePoints.value.length - 1
            context.fillStyle = current ? '#67c23a' : point.loc_conf < 0.5 ? '#e6a23c' : '#f56c6c'
            context.beginPath()
            context.arc(x, y, current ? 5 : 3, 0, Math.PI * 2)
            context.fill()
        })
        context.restore()
    },
} satisfies MapLayer

function periodLabel(period: TrackPeriod) {
    return period.start_timestamp
        ? new Date(period.start_timestamp * 1000).toLocaleString(locale.value)
        : t('log.unknownTime')
}

function periodDetail(period: TrackPeriod) {
    const elapsed =
        period.start_timestamp !== null && period.end_timestamp !== null
            ? Math.max(0, Math.round(period.end_timestamp - period.start_timestamp))
            : 0
    return `${t('log.pointCount', period.points.length)} · ${t('log.secondCount', elapsed)}`
}

async function openFile(uploadFile: UploadFile) {
    const file = uploadFile.raw
    if (!file || loading.value) return
    pause()
    loading.value = true
    errorMessage.value = ''
    try {
        if (!maps.value.length) maps.value = await api.maps()
        const result = await api.analyseLog(await file.text())
        if (!result.periods.length) throw new Error(t('log.noRecords'))
        fileName.value = file.name
        periods.value = result.periods
        warnings.value = result.warnings
        selectPeriod(result.periods[0].period_id)
    } catch (error) {
        errorMessage.value = toErrorMessage(error)
        ElMessage.error(errorMessage.value)
    } finally {
        loading.value = false
    }
}

function selectPeriod(periodId: number) {
    pause()
    selectedPeriodId.value = periodId
    frame.value = 0
}

function delayForFrame() {
    const points = selectedPeriod.value?.points
    if (!points || frame.value + 1 >= points.length) return 100
    const current = points[frame.value].timestamp
    const next = points[frame.value + 1].timestamp
    const seconds = current !== null && next !== null && next > current ? next - current : 0.1
    return Math.max(30, Math.min(500, seconds * 1000)) / speed.value
}

function schedule() {
    window.clearTimeout(timer)
    if (!playing.value) return
    timer = window.setTimeout(() => {
        if (!selectedPeriod.value || frame.value >= selectedPeriod.value.points.length - 1) return pause()
        frame.value += 1
        schedule()
    }, delayForFrame())
}

function play() {
    if (!selectedPeriod.value) return
    if (frame.value >= selectedPeriod.value.points.length - 1) frame.value = 0
    playing.value = true
    schedule()
}

function pause() {
    playing.value = false
    window.clearTimeout(timer)
}

function replay() {
    pause()
    frame.value = 0
    play()
}

function step(delta: number) {
    pause()
    if (!selectedPeriod.value) return
    frame.value = Math.max(0, Math.min(selectedPeriod.value.points.length - 1, frame.value + delta))
}

function formatTime(timestamp: number | null | undefined) {
    return timestamp ? new Date(timestamp * 1000).toLocaleTimeString(locale.value) : '--:--:--'
}

function onKeydown(event: KeyboardEvent) {
    if (!selectedPeriod.value || isInteractiveTarget(event.target)) return
    if (event.code === 'Space') {
        event.preventDefault()
        playing.value ? pause() : play()
    } else if (event.key === 'ArrowLeft') step(-1)
    else if (event.key === 'ArrowRight') step(1)
    else if (event.key === 'Home') {
        pause()
        frame.value = 0
    } else if (event.key === 'End') {
        pause()
        frame.value = selectedPeriod.value.points.length - 1
    }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => {
    pause()
    window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
    <div v-if="!selectedPeriod" class="mx-auto grid h-full max-w-2xl place-items-center p-6">
        <div class="w-full">
            <el-alert v-if="errorMessage" class="mb-4" type="error" :title="errorMessage" show-icon />
            <el-upload
                drag
                :disabled="loading"
                :auto-upload="false"
                :show-file-list="false"
                accept=".log,.txt"
                :on-change="openFile">
                <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
                <div class="el-upload__text">
                    {{ t('log.dropFile') }}<em>{{ t('log.chooseFile') }}</em>
                </div>
            </el-upload>
        </div>
    </div>

    <div v-else class="flex h-full min-h-0 flex-col md:flex-row">
        <aside
            class="flex max-h-64 w-full shrink-0 flex-col border-b border-(--el-border-color) md:max-h-none md:w-72 md:border-r md:border-b-0">
            <div class="border-b border-(--el-border-color) p-3">
                <div class="mb-2 flex items-center gap-2">
                    <el-text truncated class="min-w-0 flex-1">{{ fileName }}</el-text>
                    <el-upload
                        :disabled="loading"
                        :auto-upload="false"
                        :show-file-list="false"
                        accept=".log,.txt"
                        :on-change="openFile">
                        <el-button text :icon="FolderOpened" :loading="loading">{{ t('log.replaceFile') }}</el-button>
                    </el-upload>
                </div>
                <el-button
                    v-if="warnings.length"
                    :icon="Warning"
                    type="warning"
                    plain
                    class="w-full"
                    @click="warningDrawer = true">
                    {{ t('log.warningCount', warnings.length) }}
                </el-button>
            </div>
            <el-scrollbar class="min-h-0 flex-1">
                <el-menu
                    :default-active="String(selectedPeriodId)"
                    class="border-0!"
                    @select="selectPeriod(Number($event))">
                    <el-menu-item
                        v-for="period in periods"
                        :key="period.period_id"
                        :index="String(period.period_id)"
                        class="h-auto! min-h-14! py-2!">
                        <div class="min-w-0 leading-5">
                            <div class="truncate">{{ periodLabel(period) }}</div>
                            <el-text type="info" size="small">{{ periodDetail(period) }}</el-text>
                        </div>
                    </el-menu-item>
                </el-menu>
            </el-scrollbar>
            <el-descriptions v-if="currentPoint" :column="1" size="small" border class="m-3">
                <el-descriptions-item :label="t('common.map')">{{ currentPoint.map_name }}</el-descriptions-item>
                <el-descriptions-item :label="t('log.coordinates')"
                    ><span class="font-mono"
                        >{{ currentPoint.x.toFixed(1) }}, {{ currentPoint.y.toFixed(1) }}</span
                    ></el-descriptions-item
                >
                <el-descriptions-item :label="t('log.heading')"
                    >{{ currentPoint.rot.toFixed(1) }}°</el-descriptions-item
                >
                <el-descriptions-item :label="t('log.confidence')"
                    >{{ (currentPoint.loc_conf * 100).toFixed(1) }}%</el-descriptions-item
                >
            </el-descriptions>
        </aside>

        <main class="flex min-w-0 flex-1 flex-col">
            <div class="flex min-h-12 items-center justify-between gap-2 border-b border-(--el-border-color) px-3 py-2">
                <div class="flex min-w-0 items-center gap-2">
                    <el-button
                        :icon="playing ? VideoPause : VideoPlay"
                        :type="playing ? 'warning' : 'primary'"
                        @click="playing ? pause() : play()">
                        {{ playing ? t('log.pause') : t('log.play') }}
                    </el-button>
                    <el-button :icon="RefreshRight" @click="replay">{{ t('log.replay') }}</el-button>
                    <el-select
                        v-model="speed"
                        class="w-20! shrink-0"
                        :aria-label="t('log.playbackSpeed')"
                        @change="schedule">
                        <el-option v-for="value in [0.5, 1, 2, 4]" :key="value" :label="`${value}x`" :value="value" />
                    </el-select>
                </div>
                <div class="flex shrink-0 items-center gap-2">
                    <el-button
                        :icon="DArrowLeft"
                        :title="t('log.previousFrame')"
                        :disabled="frame === 0"
                        @click="step(-1)" />
                    <el-button
                        :icon="DArrowRight"
                        :title="t('log.nextFrame')"
                        :disabled="frame === selectedPeriod.points.length - 1"
                        @click="step(1)" />
                </div>
            </div>
            <div class="min-h-0 flex-1">
                <MapCanvas
                    v-model:tier-value="selectedLayerFile"
                    :image-url="imageUrl"
                    :image-width="currentMap?.width"
                    :image-height="currentMap?.height"
                    :viewport-key="currentMap?.id"
                    :layers="[trajectoryLayer]"
                    :tier-options="tierOptions">
                    <template #status>
                        <span class="font-mono">{{ frame + 1 }} / {{ selectedPeriod.points.length }}</span>
                    </template>
                    <template #footer>
                        <div
                            class="grid h-12 grid-cols-[5rem_1fr_5rem] items-center gap-3 border-t border-(--el-border-color) px-3 text-xs">
                            <span class="font-mono">{{ formatTime(selectedPeriod.points[0]?.timestamp) }}</span>
                            <el-slider
                                v-model="frame"
                                :min="0"
                                :max="selectedPeriod.points.length - 1"
                                :show-tooltip="false"
                                @input="pause" />
                            <span class="text-right font-mono">{{ formatTime(selectedPeriod.points.at(-1)?.timestamp) }}</span>
                        </div>
                    </template>
                </MapCanvas>
            </div>
        </main>

        <el-drawer v-model="warningDrawer" :title="t('log.warnings')" size="min(480px, 100%)">
            <el-table :data="warnings" height="100%">
                <el-table-column prop="line" :label="t('log.line')" width="80" />
                <el-table-column prop="message" :label="t('log.reason')" />
            </el-table>
        </el-drawer>
    </div>
</template>
