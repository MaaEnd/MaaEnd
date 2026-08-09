<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Connection, DataLine, Document, EditPen, Picture, Plus } from '@element-plus/icons-vue'
import { Images } from '@lucide/vue'

const { t, locale } = useI18n()
const docsLocale = computed(() => (locale.value === 'zh-CN' ? 'zh_cn' : 'en_us'))
const nodeTools = computed(() => [
    { title: t('home.newNode'), icon: Plus, route: '/node-editor?mode=new' },
    { title: t('home.editNode'), icon: EditPen, route: '/node-editor?mode=edit' },
])
const advancedTools = computed(() => [
    { title: t('routes.logAnalyser'), icon: DataLine, route: '/log-analyser' },
    { title: t('routes.navmeshEditor'), icon: Connection, route: '/navmesh-editor' },
    { title: t('routes.staticInfer'), icon: Picture, route: '/static-infer' },
    { title: t('routes.collectTestData'), icon: Images, route: '/collect-test-data' },
])
const docs = computed(() => {
    const base = `https://github.com/MaaEnd/MaaEnd/blob/v2/docs/${docsLocale.value}/developers/components`
    return [
        {
            title: t('home.readBasicDocs'),
            icon: Document,
            href: `${base}/map-tracker.md`,
        },
        {
            title: t('home.readAdvancedDocs'),
            icon: Document,
            href: `${base}/map-tracker(advanced).md`,
        },
    ]
})

function openDoc(href: string) {
    window.open(href, '_blank', 'noopener,noreferrer')
}
</script>

<template>
    <main class="mx-auto w-full max-w-2xl p-6 sm:p-10">
        <section class="mb-8">
            <h2 class="mb-3 text-sm font-medium">{{ t('home.nodeTools') }}</h2>
            <el-menu router>
                <el-menu-item v-for="action in nodeTools" :key="action.route" :index="action.route">
                    <el-icon><component :is="action.icon" /></el-icon>
                    <span>{{ action.title }}</span>
                </el-menu-item>
            </el-menu>
        </section>
        <section class="mb-8">
            <h2 class="mb-3 text-sm font-medium">{{ t('home.advancedTools') }}</h2>
            <el-menu router>
                <el-menu-item v-for="action in advancedTools" :key="action.route" :index="action.route">
                    <el-icon><component :is="action.icon" /></el-icon>
                    <span>{{ action.title }}</span>
                </el-menu-item>
            </el-menu>
        </section>
        <section>
            <h2 class="mb-3 text-sm font-medium">{{ t('home.referenceDocs') }}</h2>
            <el-menu>
                <el-menu-item
                    v-for="doc in docs"
                    :key="doc.href"
                    :index="doc.href"
                    @click="openDoc(doc.href)">
                    <el-icon><component :is="doc.icon" /></el-icon>
                    <span>{{ doc.title }}</span>
                </el-menu-item>
            </el-menu>
        </section>
    </main>
</template>
