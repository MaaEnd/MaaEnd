import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import type { EditableNode } from '@/types'

const STORAGE_KEY = 'maptracker.myNodes.v1'

function loadNodes(): EditableNode[] {
    try {
        const value = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]')
        return Array.isArray(value) ? value : []
    } catch {
        return []
    }
}

export const useNodeStore = defineStore('nodes', () => {
    const nodes = ref<EditableNode[]>(loadNodes())
    watch(nodes, (value) => localStorage.setItem(STORAGE_KEY, JSON.stringify(value)), {
        deep: true,
    })

    function save(node: EditableNode) {
        const index = nodes.value.findIndex((item) => item.id === node.id)
        const stored: EditableNode = {
            ...node,
            path: node.path.map((point) => [...point]),
            target: node.target && [...node.target],
            source: node.source && { ...node.source },
            updated_at: new Date().toISOString(),
        }
        if (index < 0) nodes.value.unshift(stored)
        else nodes.value[index] = stored
    }

    function remove(id: string) {
        nodes.value = nodes.value.filter((node) => node.id !== id)
    }

    function clear() {
        nodes.value = []
    }

    return { nodes, save, remove, clear }
})
