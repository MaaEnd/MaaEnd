import { onBeforeUnmount, onMounted, toValue, type MaybeRefOrGetter } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave } from 'vue-router'

export function useUnsavedChangesGuard(isDirty: MaybeRefOrGetter<boolean>) {
    const { t } = useI18n()

    async function confirmDiscard() {
        if (!toValue(isDirty)) return true
        try {
            await ElMessageBox.confirm(t('guard.discard'), t('common.unsaved'), {
                type: 'warning',
                confirmButtonText: t('common.confirm'),
                cancelButtonText: t('common.cancel'),
            })
            return true
        } catch {
            return false
        }
    }

    function onBeforeUnload(event: BeforeUnloadEvent) {
        if (!toValue(isDirty)) return
        event.preventDefault()
        event.returnValue = ''
    }

    onBeforeRouteLeave(async () => (await confirmDiscard()) || false)
    onMounted(() => window.addEventListener('beforeunload', onBeforeUnload))
    onBeforeUnmount(() => window.removeEventListener('beforeunload', onBeforeUnload))

    return { confirmDiscard }
}
