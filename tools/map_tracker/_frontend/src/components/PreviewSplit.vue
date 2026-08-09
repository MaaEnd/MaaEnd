<script setup lang="ts">
import { ref } from 'vue'

const PREVIEW_SPLIT_MIN = 0.2
const PREVIEW_SPLIT_MAX = 0.8

function clampRatio(value: number) {
    return Math.min(PREVIEW_SPLIT_MAX, Math.max(PREVIEW_SPLIT_MIN, value))
}

const props = withDefaults(
    defineProps<{
        topLabel?: string
        bottomLabel?: string
        initialRatio?: number
    }>(),
    {
        topLabel: '',
        bottomLabel: '',
        initialRatio: 0.5,
    },
)

const host = ref<HTMLElement | null>(null)
const ratio = ref(clampRatio(props.initialRatio))
const dragging = ref(false)

function onPointerDown(event: PointerEvent) {
    const el = host.value
    if (!el || event.button !== 0) return
    event.preventDefault()
    dragging.value = true
    const target = event.currentTarget as HTMLElement
    target.setPointerCapture(event.pointerId)
    const update = (clientY: number) => {
        const rect = el.getBoundingClientRect()
        if (rect.height <= 0) return
        ratio.value = clampRatio((clientY - rect.top) / rect.height)
    }
    update(event.clientY)
    const onMove = (moveEvent: PointerEvent) => update(moveEvent.clientY)
    const onUp = (upEvent: PointerEvent) => {
        dragging.value = false
        target.releasePointerCapture(upEvent.pointerId)
        target.removeEventListener('pointermove', onMove)
        target.removeEventListener('pointerup', onUp)
        target.removeEventListener('pointercancel', onUp)
    }
    target.addEventListener('pointermove', onMove)
    target.addEventListener('pointerup', onUp)
    target.addEventListener('pointercancel', onUp)
}
</script>

<template>
    <div
        ref="host"
        class="grid min-h-0"
        :style="{ gridTemplateRows: `${ratio}fr 0.375rem ${1 - ratio}fr` }">
        <section
            class="grid min-h-0"
            :class="topLabel ? 'grid-rows-[2.25rem_minmax(0,1fr)]' : 'grid-rows-[minmax(0,1fr)]'">
            <h2
                v-if="topLabel"
                class="flex items-center px-4 text-xs font-medium text-(--el-text-color-secondary)">
                {{ topLabel }}
            </h2>
            <div class="min-h-0 overflow-hidden">
                <slot name="top" />
            </div>
        </section>

        <div
            role="separator"
            aria-orientation="horizontal"
            :aria-valuenow="Math.round(ratio * 100)"
            aria-valuemin="20"
            aria-valuemax="80"
            class="z-10 cursor-row-resize bg-(--el-border-color) transition-colors hover:bg-(--el-color-primary) active:bg-(--el-color-primary)"
            :class="dragging ? 'bg-(--el-color-primary)' : ''"
            @pointerdown="onPointerDown" />

        <section
            class="grid min-h-0"
            :class="bottomLabel ? 'grid-rows-[2.25rem_minmax(0,1fr)]' : 'grid-rows-[minmax(0,1fr)]'">
            <h2
                v-if="bottomLabel"
                class="flex items-center px-4 text-xs font-medium text-(--el-text-color-secondary)">
                {{ bottomLabel }}
            </h2>
            <div class="min-h-0 overflow-hidden">
                <slot name="bottom" />
            </div>
        </section>
    </div>
</template>
