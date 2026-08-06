<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ImagePlus, RefreshCw, Trash2 } from '@lucide/vue'
import MapCanvas from '@/components/MapCanvas.vue'
import ImageView from '@/components/ImageView.vue'
import PreviewSplit from '@/components/PreviewSplit.vue'
import { drawLocationPose, type MapLayer } from '@/utils/canvas'
import { api } from '@/api'
import type { MapInfo, StaticInferHistoryItem, StaticInferResult } from '@/types'
import { errorMessage, findLayerByName, findMapByName } from '@/utils/misc'

const { t, locale } = useI18n()
const maps = ref<MapInfo[]>([])
const mapError = ref('')
const history = ref<StaticInferHistoryItem[]>([])
const selectedId = ref<string | null>(null)
const precision = ref(0.7)
const inferring = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

const selected = computed(() => history.value.find((item) => item.id === selectedId.value) || null)
const result = computed(() => selected.value?.result || null)
const selectedMap = computed(() => findMapByName(maps.value, result.value?.map_name || ''))
const selectedLayer = computed(() => findLayerByName(selectedMap.value, result.value?.map_name || ''))
const mapImageUrl = computed(() => (selectedLayer.value ? api.mapImage(selectedLayer.value.file_name) : ''))
const focusPoint = computed<[number, number] | undefined>(() =>
    result.value ? [result.value.x, result.value.y] : undefined,
)
const locationLayers = computed<MapLayer[]>(() => {
    const current = result.value
    if (!current) return []
    return [
        {
            id: 'static-infer-location',
            zIndex: 20,
            draw(context, viewport) {
                drawLocationPose(context, viewport, current.x, current.y, current.rot)
            },
        },
    ]
})

function formatTime(timestamp: number) {
    return new Date(timestamp).toLocaleString(locale.value)
}

function formatPercent(value: number) {
    return `${Math.round(value * 100)}%`
}

function clampPrecision(value: number) {
    return Math.min(1, Math.max(0.1, Math.round(value * 10) / 10))
}

function patchHistoryItem(id: string, patch: Partial<StaticInferHistoryItem>) {
    const index = history.value.findIndex((entry) => entry.id === id)
    if (index < 0) return
    history.value[index] = { ...history.value[index], ...patch }
}

async function onFilesSelected(event: Event) {
    const input = event.target as HTMLInputElement
    const files = Array.from(input.files || [])
    input.value = ''
    for (const file of files) {
        const id = crypto.randomUUID()
        const usedPrecision = clampPrecision(precision.value)
        history.value.unshift({
            id,
            filename: file.name,
            file,
            imageUrl: URL.createObjectURL(file),
            precision: usedPrecision,
            result: null,
            error: null,
            createdAt: Date.now(),
        })
        selectedId.value = id
        precision.value = usedPrecision
        await runInfer(id, file, usedPrecision)
    }
}

async function rerunSelected() {
    const item = selected.value
    if (!item || inferring.value) return
    const usedPrecision = clampPrecision(precision.value)
    patchHistoryItem(item.id, { error: null, result: null })
    await runInfer(item.id, item.file, usedPrecision)
}

async function runInfer(itemId: string, file: File, usedPrecision: number) {
    const precisionForRun = clampPrecision(usedPrecision)
    patchHistoryItem(itemId, { precision: precisionForRun })
    inferring.value = true
    try {
        const next = await api.inferImage(file, precisionForRun)
        if (!history.value.some((entry) => entry.id === itemId)) return
        patchHistoryItem(itemId, {
            precision: next.precision,
            result: next,
            error: null,
            createdAt: Date.now(),
        })
        if (selectedId.value === itemId) precision.value = next.precision
    } catch (error) {
        const message = errorMessage(error)
        patchHistoryItem(itemId, { error: message, result: null })
        if (selectedId.value === itemId) ElMessage.error(message)
    } finally {
        inferring.value = false
    }
}

function removeSelected() {
    const item = selected.value
    if (!item) return
    URL.revokeObjectURL(item.imageUrl)
    history.value = history.value.filter((entry) => entry.id !== item.id)
    selectedId.value = history.value[0]?.id || null
}

function resultRows(value: StaticInferResult) {
    return [
        { label: t('staticInfer.precision'), value: value.precision.toFixed(1) },
        { label: t('staticInfer.mapName'), value: value.map_name },
        { label: t('staticInfer.position'), value: `${value.x.toFixed(1)}, ${value.y.toFixed(1)}` },
        { label: t('staticInfer.heading'), value: `${value.rot}°` },
        { label: t('staticInfer.locConf'), value: formatPercent(value.loc_conf) },
        { label: t('staticInfer.rotConf'), value: formatPercent(value.rot_conf) },
        { label: t('staticInfer.inferTime'), value: `${value.infer_time_ms} ms` },
        { label: t('staticInfer.locTime'), value: `${value.loc_time_ms} ms` },
        { label: t('staticInfer.rotTime'), value: `${value.rot_time_ms} ms` },
    ]
}

watch(selectedId, (id) => {
    const item = history.value.find((entry) => entry.id === id)
    if (item) precision.value = item.precision
})

onMounted(async () => {
    try {
        maps.value = await api.maps()
    } catch (error) {
        mapError.value = errorMessage(error)
    }
})

onBeforeUnmount(() => {
    for (const item of history.value) URL.revokeObjectURL(item.imageUrl)
})
</script>

<template>
    <section class="flex h-full min-h-0 flex-col">
        <div
            class="grid min-h-0 flex-1 grid-cols-[300px_minmax(0,1fr)] max-md:grid-cols-1 max-md:grid-rows-[minmax(180px,34vh)_minmax(0,1fr)]">
            <aside class="flex min-h-0 flex-col border-r border-(--el-border-color) max-md:border-r-0 max-md:border-b">
                <div class="shrink-0 border-b border-(--el-border-color) p-3">
                    <el-button
                        type="primary"
                        class="w-full"
                        :icon="ImagePlus"
                        :loading="inferring"
                        @click="fileInput?.click()">
                        {{ t('staticInfer.chooseImage') }}
                    </el-button>
                    <input
                        ref="fileInput"
                        class="hidden"
                        type="file"
                        accept="image/*"
                        multiple
                        @change="onFilesSelected" />
                </div>

                <el-scrollbar v-if="history.length" class="min-h-0 flex-1">
                    <button
                        v-for="item in history"
                        :key="item.id"
                        type="button"
                        class="grid w-full grid-cols-1 gap-1 border-b border-(--el-border-color-lighter) px-4 py-3 text-left hover:bg-(--el-fill-color-light)"
                        :class="item.id === selectedId ? 'bg-(--el-color-primary-light-9)' : ''"
                        @click="selectedId = item.id">
                        <strong class="truncate text-sm font-medium">{{ item.filename }}</strong>
                        <span class="truncate font-mono text-xs text-(--el-text-color-regular)">
                            <template v-if="item.result">
                                {{ item.result.map_name }} · {{ item.result.x.toFixed(1) }},
                                {{ item.result.y.toFixed(1) }}
                            </template>
                            <template v-else-if="item.error">{{ t('staticInfer.failed') }}</template>
                            <template v-else>{{ t('staticInfer.pending') }}</template>
                        </span>
                        <time class="text-xs text-(--el-text-color-secondary)">{{ formatTime(item.createdAt) }}</time>
                    </button>
                </el-scrollbar>

                <el-empty
                    v-else
                    class="min-h-0 flex-1"
                    :description="t('staticInfer.emptyHistory')"
                    :image-size="64" />
            </aside>

            <main
                v-if="selected"
                class="grid min-h-0 grid-cols-[minmax(0,1fr)_300px] max-xl:grid-cols-1 max-xl:grid-rows-[minmax(360px,1fr)_auto]">
                <PreviewSplit :top-label="t('staticInfer.sourceImage')" :bottom-label="t('samples.locationMap')">
                    <template #top>
                        <ImageView :src="selected.imageUrl" :alt="selected.filename" />
                    </template>
                    <template #bottom>
                        <el-empty
                            v-if="selected.error"
                            class="min-h-0 bg-neutral-900"
                            :description="selected.error"
                            :image-size="0" />
                        <el-empty
                            v-else-if="!result"
                            class="min-h-0 bg-neutral-900"
                            :description="inferring ? t('staticInfer.running') : t('staticInfer.noResult')"
                            :image-size="0" />
                        <el-empty
                            v-else-if="mapError || !selectedMap || !selectedLayer"
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
                                    {{ result.x.toFixed(1) }}, {{ result.y.toFixed(1) }} · {{ result.rot }}°
                                </span>
                            </template>
                        </MapCanvas>
                    </template>
                </PreviewSplit>

                <aside
                    class="flex flex-col border-l border-(--el-border-color) p-4 max-xl:max-h-[42vh] max-xl:overflow-y-auto max-xl:border-l-0 max-xl:border-t">
                    <div class="min-w-0">
                        <h2 class="truncate text-sm font-medium" :title="selected.filename">{{ selected.filename }}</h2>
                        <p class="mt-1 text-xs text-(--el-text-color-secondary)">
                            {{ formatTime(selected.createdAt) }}
                        </p>
                    </div>

                    <el-alert
                        v-if="selected.error"
                        class="mt-4"
                        type="error"
                        :title="selected.error"
                        :closable="false"
                        show-icon />

                    <dl v-if="result" class="mt-4 grid gap-2 text-sm">
                        <div
                            v-for="row in resultRows(result)"
                            :key="row.label"
                            class="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
                            <dt class="text-(--el-text-color-secondary)">{{ row.label }}</dt>
                            <dd class="truncate font-mono" :title="row.value">{{ row.value }}</dd>
                        </div>
                    </dl>
                    <el-empty
                        v-else-if="!selected.error"
                        class="mt-4"
                        :description="inferring ? t('staticInfer.running') : t('staticInfer.noResult')"
                        :image-size="48" />

                    <div class="mt-auto flex flex-col gap-2 max-xl:mt-4">
                        <el-form label-position="top" class="mb-1">
                            <el-form-item :label="t('staticInfer.precision')" class="!mb-0">
                                <el-slider
                                    v-model="precision"
                                    class="w-full px-1"
                                    :min="0.1"
                                    :max="1"
                                    :step="0.1"
                                    :show-tooltip="true"
                                    :format-tooltip="(value: number) => clampPrecision(value).toFixed(1)"
                                    @change="(value: number | number[]) => {
                                        precision = clampPrecision(Array.isArray(value) ? value[0] : value)
                                    }" />
                            </el-form-item>
                        </el-form>
                        <el-button
                            class="w-full"
                            type="primary"
                            :icon="RefreshCw"
                            :loading="inferring"
                            @click="rerunSelected">
                            {{ t('staticInfer.rerun') }}
                        </el-button>
                        <el-button
                            class="ml-0! w-full"
                            type="danger"
                            plain
                            :icon="Trash2"
                            @click="removeSelected">
                            {{ t('staticInfer.remove') }}
                        </el-button>
                    </div>
                </aside>
            </main>

            <el-empty v-else class="min-h-0" :description="t('staticInfer.selectOrAdd')" :image-size="72" />
        </div>
    </section>
</template>
