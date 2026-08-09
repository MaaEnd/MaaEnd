<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api'
import type { MapInfo, MapLayerInfo } from '@/types'

const props = defineProps<{
    modelValue: string
    maps: MapInfo[]
}>()

const emit = defineEmits<{
    'update:modelValue': [value: string]
}>()

const { t } = useI18n()
const regionFilter = ref('')
const failedThumbs = ref(new Set<string>())

function regionOf(map: MapInfo) {
    return map.id.split('_')[0] || map.id
}

function previewLayer(map: MapInfo): MapLayerInfo | undefined {
    return map.layers.find((layer) => layer.id === 'main') || map.layers[0]
}

function previewUrl(map: MapInfo) {
    const layer = previewLayer(map)
    return layer ? api.mapImage(layer.file_name) : ''
}

function markFailed(mapId: string) {
    const next = new Set(failedThumbs.value)
    next.add(mapId)
    failedThumbs.value = next
}

const regions = computed(() => {
    const ids = new Set(props.maps.map(regionOf))
    return [...ids].sort()
})

const filteredMaps = computed(() => {
    if (!regionFilter.value) return props.maps
    return props.maps.filter((map) => regionOf(map) === regionFilter.value)
})

function select(mapId: string) {
    emit('update:modelValue', mapId)
}
</script>

<template>
    <div class="flex flex-col gap-3">
        <div class="flex flex-wrap gap-1.5">
            <el-check-tag
                :checked="!regionFilter"
                class="cursor-pointer"
                @change="regionFilter = ''">
                {{ t('mapPicker.allRegions') }}
            </el-check-tag>
            <el-check-tag
                v-for="region in regions"
                :key="region"
                :checked="regionFilter === region"
                class="cursor-pointer"
                @change="regionFilter = regionFilter === region ? '' : region">
                {{ region }}
            </el-check-tag>
        </div>
        <div
            v-if="filteredMaps.length"
            class="grid max-h-72 grid-cols-2 gap-2 overflow-y-auto sm:grid-cols-3">
            <button
                v-for="map in filteredMaps"
                :key="map.id"
                type="button"
                class="overflow-hidden rounded-md border text-left transition-colors"
                :class="
                    modelValue === map.id
                        ? 'border-(--el-color-primary) bg-(--el-color-primary-light-9)'
                        : 'border-(--el-border-color) hover:border-(--el-color-primary-light-5)'
                "
                @click="select(map.id)">
                <div class="bg-(--el-fill-color-light) aspect-video w-full overflow-hidden">
                    <img
                        v-if="previewUrl(map) && !failedThumbs.has(map.id)"
                        :src="previewUrl(map)"
                        :alt="map.name"
                        loading="lazy"
                        class="h-full w-full object-cover"
                        @error="markFailed(map.id)" />
                    <div
                        v-else
                        class="text-(--el-text-color-placeholder) flex h-full items-center justify-center text-xs">
                        {{ map.name }}
                    </div>
                </div>
                <div class="flex items-baseline justify-between gap-2 p-2">
                    <div class="min-w-0 truncate text-sm font-medium" :title="map.name">{{ map.name }}</div>
                    <div class="text-(--el-text-color-secondary) shrink-0 text-xs">
                        {{ t('mapPicker.layerCount', { n: map.layers.length }) }}
                    </div>
                </div>
            </button>
        </div>
        <el-empty v-else :description="t('mapPicker.empty')" :image-size="64" />
    </div>
</template>
