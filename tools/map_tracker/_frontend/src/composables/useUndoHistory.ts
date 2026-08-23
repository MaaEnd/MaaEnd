import { ref, type Ref } from 'vue'

export function useUndoHistory<T>(
    capture: () => T | undefined,
    restore: (state: T) => void,
    isDisabled: () => boolean,
) {
    const history = ref<T[]>([]) as Ref<T[]>
    const future = ref<T[]>([]) as Ref<T[]>
    let discardedFuture: T[] | undefined
    let discardedHistoryHead: T[] = []

    function snapshot() {
        const state = capture()
        if (state === undefined) return
        discardedHistoryHead = history.value.length >= 100 ? history.value.splice(0, 1) : []
        history.value.push(state)
        discardedFuture = future.value
        future.value = []
    }

    function undo() {
        if (isDisabled()) return
        const current = capture()
        if (current === undefined || !history.value.length) return
        discardedFuture = undefined
        discardedHistoryHead = []
        future.value.push(current)
        restore(history.value.pop()!)
    }

    function redo() {
        if (isDisabled()) return
        const current = capture()
        if (current === undefined || !future.value.length) return
        discardedFuture = undefined
        discardedHistoryHead = []
        history.value.push(current)
        restore(future.value.pop()!)
    }

    function clear() {
        history.value = []
        future.value = []
        discardedFuture = undefined
        discardedHistoryHead = []
    }

    function discardSnapshot() {
        history.value.pop()
        history.value.unshift(...discardedHistoryHead)
        if (discardedFuture) future.value = discardedFuture
        discardedFuture = undefined
        discardedHistoryHead = []
    }

    return { history, future, snapshot, undo, redo, clear, discardSnapshot }
}
