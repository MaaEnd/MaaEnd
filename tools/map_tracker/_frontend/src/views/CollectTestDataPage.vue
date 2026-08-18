<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { FolderOpen, Play, Save, Square } from '@lucide/vue'
import MapCanvas from '@/components/MapCanvas.vue'
import ImageView from '@/components/ImageView.vue'
import PreviewSplit from '@/components/PreviewSplit.vue'
import { drawLocationPose, type MapLayer } from '@/utils/canvas'
import { api } from '@/api'
import type { MapInfo, SampleCollectionStatus, SampleItem, SampleKind, SampleMetadata } from '@/types'
import { errorMessage, findLayerByName, findMapByName } from '@/utils/misc'

const { t, locale } = useI18n()
const defaultOutputDirectory = 'tests/MaaEndTestset/Win32/Official_CN/map_tracker'

const outputDirectory = ref(defaultOutputDirectory)
const status = ref<SampleCollectionStatus>({
    running: false,
    output_dir: '',
    candidate_limit: 30,
    error: null,
    candidates: [],
    existing: [],
})
const activeTab = ref<SampleKind>('candidate')
const selectedIds = ref<Record<SampleKind, string | null>>({ candidate: null, existing: null })
const loading = ref(false)
const submitting = ref(false)
const requestError = ref('')
const pollError = ref('')
const mapError = ref('')
const maps = ref<MapInfo[]>([])
const metadataDraft = ref<SampleMetadata>({ map_name: '', x: 0, y: 0, rot: 0 })
let requestVersion = 0
let refreshing = false
let pollTimer: number | undefined

const activeItems = computed(() =>
    activeTab.value === 'candidate' ? status.value.candidates : status.value.existing,
)
const selected = computed<SampleItem | null>(() => {
    const selectedId = selectedIds.value[activeTab.value]
    return activeItems.value.find((item) => item.id === selectedId) || null
})
const displayedSample = computed<SampleItem | null>(() => {
    if (!selected.value) return null
    return { ...selected.value, ...metadataDraft.value }
})
const sampleImageUrl = computed(() =>
    selected.value ? `${api.sampleImage(selected.value.id)}?v=${selected.value.timestamp}` : '',
)
const selectedMap = computed(() => findMapByName(maps.value, displayedSample.value?.map_name || ''))
const selectedLayer = computed(() => findLayerByName(selectedMap.value, displayedSample.value?.map_name || ''))
const mapImageUrl = computed(() => (selectedLayer.value ? api.mapImage(selectedLayer.value.file_name) : ''))
const focusPoint = computed<[number, number] | undefined>(() =>
    displayedSample.value ? [displayedSample.value.x, displayedSample.value.y] : undefined,
)
const locationLayers = computed<MapLayer[]>(() => {
    const sample = displayedSample.value
    if (!sample) return []
    return [
        {
            id: 'sample-location',
            zIndex: 20,
            draw(context, viewport) {
                drawLocationPose(context, viewport, sample.x, sample.y, sample.rot)
            },
        },
    ]
})
const mapOptions = computed(() => {
    const names = maps.value.flatMap((map) => map.layers.map((layer) => layer.map_name))
    return [...new Set(names)].sort()
})
const metadataValid = computed(
    () =>
        Boolean(metadataDraft.value.map_name.trim()) &&
        Number.isFinite(metadataDraft.value.x) &&
        Number.isFinite(metadataDraft.value.y) &&
        Number.isInteger(metadataDraft.value.rot) &&
        metadataDraft.value.rot >= 0 &&
        metadataDraft.value.rot < 360,
)
const metadataChanged = computed(() => {
    const sample = selected.value
    if (!sample) return false
    return (
        metadataDraft.value.map_name !== sample.map_name ||
        metadataDraft.value.x !== sample.x ||
        metadataDraft.value.y !== sample.y ||
        metadataDraft.value.rot !== sample.rot
    )
})
const canSubmit = computed(
    () =>
        Boolean(selected.value) &&
        metadataValid.value &&
        (selected.value?.kind === 'candidate' || metadataChanged.value),
)
const submitLabel = computed(() =>
    selected.value?.kind === 'candidate' ? t('samples.saveCandidate') : t('samples.saveMetadata'),
)
const statusError = computed(() => requestError.value || pollError.value || status.value.error || '')

function sampleMetadataOf(sample: SampleItem): SampleMetadata {
    return {
        map_name: sample.map_name,
        x: sample.x,
        y: sample.y,
        rot: sample.rot,
    }
}

function syncStatus(nextStatus: SampleCollectionStatus, selectNewest = true) {
    const previousIds = new Set(status.value.candidates.map((candidate) => candidate.id))
    status.value = nextStatus
    const candidateId = selectedIds.value.candidate
    if (!candidateId || !nextStatus.candidates.some((item) => item.id === candidateId)) {
        const newest = nextStatus.candidates.find((item) => selectNewest && !previousIds.has(item.id))
        selectedIds.value.candidate = newest?.id || nextStatus.candidates[0]?.id || null
    }
    const existingId = selectedIds.value.existing
    if (!existingId || !nextStatus.existing.some((item) => item.id === existingId)) {
        selectedIds.value.existing = nextStatus.existing[0]?.id || null
    }
}

async function runBusy(busy: { value: boolean }, action: () => Promise<void>, exclusive = false) {
    if (exclusive && busy.value) return
    busy.value = true
    requestVersion += 1
    requestError.value = ''
    try {
        await action()
    } catch (error) {
        requestError.value = errorMessage(error)
    } finally {
        busy.value = false
    }
}

async function refresh() {
    if (refreshing) return
    refreshing = true
    const version = requestVersion
    try {
        const nextStatus = await api.sampleCollectionStatus()
        if (version === requestVersion) {
            syncStatus(nextStatus)
            pollError.value = ''
        }
    } catch (error) {
        if (version === requestVersion) pollError.value = errorMessage(error)
    } finally {
        refreshing = false
    }
}

async function loadDirectory(directory = outputDirectory.value.trim()) {
    if (!directory || status.value.running) return
    await runBusy(loading, async () => {
        syncStatus(await api.loadSampleDirectory(directory), false)
    })
}

async function startCollection() {
    const directory = outputDirectory.value.trim()
    if (!directory) return
    await runBusy(loading, async () => {
        syncStatus(await api.startSampleCollection(directory), false)
    })
}

async function stopCollection() {
    await runBusy(loading, async () => {
        syncStatus(await api.stopSampleCollection(), false)
    })
}

async function saveCandidateItem(item: SampleItem) {
    if (item.kind !== 'candidate') return
    const usingDraft = item.id === selected.value?.id
    if (usingDraft && !metadataValid.value) return
    const metadata = usingDraft ? { ...metadataDraft.value } : sampleMetadataOf(item)
    await runBusy(
        submitting,
        async () => {
            const result = await api.saveSample(item.id, metadata)
            syncStatus(result.status, false)
            ElMessage.success(t('samples.savedAs', { filename: result.saved.filename }))
        },
        true,
    )
}

async function submitSelected() {
    const sample = selected.value
    if (!sample || !canSubmit.value) return
    if (sample.kind === 'candidate') {
        await saveCandidateItem(sample)
        return
    }
    await runBusy(
        submitting,
        async () => {
            const result = await api.updateSample(sample.id, metadataDraft.value)
            syncStatus(result.status, false)
            selectedIds.value.existing = result.updated.id
            metadataDraft.value = sampleMetadataOf(result.updated)
            ElMessage.success(t('samples.metadataSaved'))
        },
        true,
    )
}

function formatTime(timestamp: number, withDate = false): string {
    return new Intl.DateTimeFormat(locale.value, {
        ...(withDate
            ? { year: 'numeric', month: '2-digit', day: '2-digit' }
            : {}),
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    }).format(new Date(timestamp * 1000))
}

watch(
    () => selected.value?.id,
    () => {
        if (selected.value) metadataDraft.value = sampleMetadataOf(selected.value)
    },
)

onMounted(async () => {
    const mapRequest = api
        .maps()
        .then((result) => {
            maps.value = result
            mapError.value = ''
        })
        .catch((error) => {
            mapError.value = errorMessage(error)
        })
    const sampleRequest = (async () => {
        try {
            const nextStatus = await api.sampleCollectionStatus()
            if (nextStatus.running || nextStatus.output_dir) {
                syncStatus(nextStatus, false)
                return
            }
            const directory = outputDirectory.value.trim()
            if (!directory) return
            syncStatus(await api.loadSampleDirectory(directory), false)
        } catch (error) {
            requestError.value = errorMessage(error)
        }
    })()
    await Promise.all([sampleRequest, mapRequest])
    pollTimer = window.setInterval(refresh, 1000)
})

onBeforeUnmount(() => {
    if (pollTimer !== undefined) window.clearInterval(pollTimer)
})
</script>

<template>
    <section class="flex h-full min-h-0 flex-col bg-(--el-bg-color)">
        <header class="flex flex-wrap items-end gap-3 border-b border-(--el-border-color) px-4 py-3">
            <label class="min-w-64 flex-1">
                <span class="mb-1.5 block text-xs text-(--el-text-color-secondary)">
                    {{ t('samples.outputDirectory') }}
                </span>
                <el-input
                    v-model="outputDirectory"
                    :disabled="status.running || loading"
                    @keyup.enter="loadDirectory()">
                    <template #prefix><FolderOpen :size="16" /></template>
                </el-input>
            </label>

            <el-button
                v-if="!status.running"
                type="primary"
                :loading="loading"
                :disabled="!outputDirectory.trim()"
                @click="startCollection">
                <Play v-if="!loading" :size="16" />
                <span>{{ t('common.start') }}</span>
            </el-button>
            <el-button v-else type="danger" plain :loading="loading" @click="stopCollection">
                <Square v-if="!loading" :size="15" />
                <span>{{ t('common.stop') }}</span>
            </el-button>

            <div class="flex min-h-8 items-center gap-2 text-sm">
                <span
                    class="size-2 rounded-full"
                    :class="status.running ? 'bg-(--el-color-success)' : 'bg-(--el-text-color-placeholder)'" />
                <span>{{ status.running ? t('samples.collecting') : t('samples.stopped') }}</span>
                <span class="text-(--el-text-color-secondary)">
                    {{ t('samples.existingCount', { count: status.existing.length }) }}
                </span>
            </div>
        </header>

        <el-alert
            v-if="statusError"
            class="rounded-none! border-b border-(--el-color-danger-light-5)"
            type="error"
            :title="statusError"
            :closable="false"
            show-icon />

        <div class="grid min-h-0 flex-1 grid-cols-[300px_minmax(0,1fr)] max-md:grid-cols-1 max-md:grid-rows-[minmax(180px,34vh)_minmax(0,1fr)]">
            <aside class="flex min-h-0 flex-col border-r border-(--el-border-color) max-md:border-r-0 max-md:border-b">
                <div class="shrink-0 border-b border-(--el-border-color) px-3 pt-1">
                    <el-tabs v-model="activeTab" stretch>
                        <el-tab-pane name="candidate">
                            <template #label>
                                <span>{{ t('samples.candidateTab') }}</span>
                                <span class="ml-1.5 font-mono text-xs">
                                    {{ status.candidates.length }}/{{ status.candidate_limit }}
                                </span>
                            </template>
                        </el-tab-pane>
                        <el-tab-pane name="existing">
                            <template #label>
                                <span>{{ t('samples.existingTab') }}</span>
                                <span class="ml-1.5 font-mono text-xs">{{ status.existing.length }}</span>
                            </template>
                        </el-tab-pane>
                    </el-tabs>
                </div>

                <el-scrollbar v-if="activeItems.length" class="min-h-0 flex-1">
                    <button
                        v-for="item in activeItems"
                        :key="item.id"
                        type="button"
                        class="grid w-full gap-x-3 border-b border-(--el-border-color-lighter) px-4 py-3 text-left hover:bg-(--el-fill-color-light)"
                        :class="[
                            item.kind === 'candidate' ? 'grid-cols-[minmax(0,1fr)_auto]' : 'grid-cols-1',
                            item.id === selectedIds[activeTab] ? 'bg-(--el-color-primary-light-9)' : '',
                        ]"
                        @click="selectedIds[activeTab] = item.id"
                        @dblclick="saveCandidateItem(item)">
                        <strong class="truncate text-sm font-medium">{{ item.map_name }}</strong>
                        <time
                            v-if="item.kind === 'candidate'"
                            class="text-xs text-(--el-text-color-secondary)">
                            {{ formatTime(item.timestamp) }}
                        </time>
                        <span class="truncate font-mono text-xs text-(--el-text-color-regular)">
                            {{ item.x.toFixed(1) }}, {{ item.y.toFixed(1) }} · {{ item.rot }}°
                        </span>
                        <span
                            v-if="item.inference"
                            class="font-mono text-xs text-(--el-text-color-secondary)">
                            {{ Math.round(item.inference.loc_conf * 100) }}% ·
                            {{ Math.round(item.inference.rot_conf * 100) }}%
                        </span>
                    </button>
                </el-scrollbar>

                <el-empty
                    v-else
                    class="min-h-0 flex-1"
                    :description="activeTab === 'candidate'
                        ? (status.running ? t('samples.noCandidatesActive') : t('samples.noCandidatesStopped'))
                        : (status.output_dir ? t('samples.noExisting') : t('samples.existingNotLoaded'))"
                    :image-size="64" />
            </aside>

            <main v-if="selected" class="grid min-h-0 grid-cols-[minmax(0,1fr)_300px] max-xl:grid-cols-1 max-xl:grid-rows-[minmax(360px,1fr)_auto]">
                <PreviewSplit :top-label="t('samples.sampleImage')" :bottom-label="t('samples.locationMap')">
                    <template #top>
                        <ImageView :src="sampleImageUrl" :alt="selected.filename" />
                    </template>
                    <template #bottom>
                        <el-empty
                            v-if="mapError || !selectedMap || !selectedLayer"
                            class="min-h-0 bg-neutral-900"
                            :description="mapError || t('samples.mapUnavailable')"
                            :image-size="0" />
                        <MapCanvas
                            v-else
                            :image-url="mapImageUrl"
                            :image-width="selectedMap.width"
                            :image-height="selectedMap.height"
                            :viewport-key="selectedLayer.file_name"
                            :layers="locationLayers"
                            :focus-point="focusPoint">
                            <template #status>
                                <span class="font-mono">
                                    {{ displayedSample?.x.toFixed(1) }}, {{ displayedSample?.y.toFixed(1) }} ·
                                    {{ displayedSample?.rot }}°
                                </span>
                            </template>
                        </MapCanvas>
                    </template>
                </PreviewSplit>

                <aside class="flex flex-col border-l border-(--el-border-color) p-4 max-xl:max-h-[42vh] max-xl:overflow-y-auto max-xl:border-l-0 max-xl:border-t">
                    <div class="min-w-0">
                        <h2 class="truncate text-sm font-medium" :title="selected.filename">{{ selected.filename }}</h2>
                    </div>

                    <el-form class="mt-5" label-position="top" @submit.prevent="submitSelected">
                        <el-form-item :label="t('samples.mapName')">
                            <el-select
                                v-model="metadataDraft.map_name"
                                class="w-full"
                                filterable
                                allow-create
                                default-first-option>
                                <el-option v-for="mapName in mapOptions" :key="mapName" :label="mapName" :value="mapName" />
                            </el-select>
                        </el-form-item>
                        <div class="grid grid-cols-2 gap-x-3">
                            <el-form-item :label="t('samples.xCoordinate')">
                                <el-input-number v-model="metadataDraft.x" class="w-full" :precision="1" :step="0.1" controls-position="right" />
                            </el-form-item>
                            <el-form-item :label="t('samples.yCoordinate')">
                                <el-input-number v-model="metadataDraft.y" class="w-full" :precision="1" :step="0.1" controls-position="right" />
                            </el-form-item>
                        </div>
                        <el-form-item :label="t('samples.heading')">
                            <el-input-number
                                v-model="metadataDraft.rot"
                                class="w-full"
                                :min="0"
                                :max="359"
                                :precision="0"
                                controls-position="right" />
                        </el-form-item>
                        <p class="text-xs text-(--el-text-color-secondary)">
                            {{ t('samples.updatedAt') }} · {{ formatTime(selected.timestamp, true) }}
                        </p>
                    </el-form>

                    <el-button
                        class="mt-auto w-full max-xl:mt-4 max-xl:w-auto max-xl:self-end"
                        type="primary"
                        :icon="Save"
                        :loading="submitting"
                        :disabled="!canSubmit"
                        @click="submitSelected">
                        {{ submitLabel }}
                    </el-button>
                </aside>
            </main>

            <el-empty
                v-else
                class="min-h-0"
                :description="activeTab === 'candidate' ? t('samples.selectCandidate') : t('samples.selectExisting')"
                :image-size="72" />
        </div>
    </section>
</template>
