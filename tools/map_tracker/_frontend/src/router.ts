import { createRouter, createWebHistory } from 'vue-router'

export default createRouter({
    history: createWebHistory(import.meta.env.BASE_URL),
    routes: [
        { path: '/', name: 'home', meta: { titleKey: 'routes.tools' }, component: () => import('@/views/HomePage.vue') },
        {
            path: '/node-editor',
            name: 'node-editor',
            meta: { titleKey: 'routes.nodeEditor' },
            component: () => import('@/views/NodeEditorPage.vue'),
        },
        {
            path: '/log-analyser',
            name: 'log-analyser',
            meta: { titleKey: 'routes.logAnalyser' },
            component: () => import('@/views/LogAnalyserPage.vue'),
        },
        {
            path: '/navmesh-editor',
            name: 'navmesh-editor',
            meta: { titleKey: 'routes.navmeshEditor' },
            component: () => import('@/views/NavMeshEditorPage.vue'),
        },
        {
            path: '/static-infer',
            name: 'static-infer',
            meta: { titleKey: 'routes.staticInfer' },
            component: () => import('@/views/StaticInferPage.vue'),
        },
        {
            path: '/collect-test-data',
            name: 'collect-test-data',
            meta: { titleKey: 'routes.collectTestData' },
            component: () => import('@/views/CollectTestDataPage.vue'),
        },
    ],
})
