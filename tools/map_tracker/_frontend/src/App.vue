<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import en from 'element-plus/es/locale/lang/en'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import { Check, HomeFilled } from '@element-plus/icons-vue'
import { Languages } from '@lucide/vue'
import { setLocale, type SupportedLocale } from '@/i18n'

const route = useRoute()
const router = useRouter()
const { locale, t } = useI18n()
const language = computed(() => locale.value as SupportedLocale)
const elementLocale = computed(() => (language.value === 'zh-CN' ? zhCn : en))
const routeTitle = computed(() => t(String(route.meta.titleKey || 'routes.tools')))

watchEffect(() => {
    document.title = route.name === 'home' ? t('app.title') : `${t('app.title')} | ${routeTitle.value}`
})

function selectLanguage(value: string | number | object) {
    setLocale(value as SupportedLocale)
}
</script>

<template>
    <el-config-provider :locale="elementLocale">
        <el-container class="h-full bg-(--el-bg-color-page) text-(--el-text-color-primary)">
            <el-header class="flex h-12! items-center gap-2 border-b border-(--el-border-color) px-3!">
                <el-page-header
                    v-if="route.name !== 'home'"
                    class="min-w-0 flex-1 overflow-hidden [&_.el-page-header__content]:truncate [&_.el-page-header__left]:min-w-0 [&_.el-page-header__title]:hidden [&_.el-divider]:hidden sm:[&_.el-page-header__title]:block sm:[&_.el-divider]:block"
                    title="MapTracker"
                    :content="routeTitle"
                    @back="router.push('/')" />
                <div
                    v-else
                    class="text-(--el-text-color-primary) flex min-w-0 flex-1 items-center gap-2 text-sm font-medium select-none">
                    <el-icon><HomeFilled /></el-icon>
                    <span class="truncate">MapTracker</span>
                </div>
                <el-dropdown trigger="click" @command="selectLanguage">
                    <el-button text circle :title="t('app.language')" :aria-label="t('app.language')">
                        <Languages :size="17" :stroke-width="1.8" />
                    </el-button>
                    <template #dropdown>
                        <el-dropdown-menu>
                            <el-dropdown-item command="zh-CN">
                                <el-icon><Check v-if="language === 'zh-CN'" /></el-icon>
                                简体中文
                            </el-dropdown-item>
                            <el-dropdown-item command="en-US">
                                <el-icon><Check v-if="language === 'en-US'" /></el-icon>
                                English
                            </el-dropdown-item>
                        </el-dropdown-menu>
                    </template>
                </el-dropdown>
            </el-header>
            <el-main class="min-h-0 p-0!"><router-view /></el-main>
        </el-container>
    </el-config-provider>
</template>
