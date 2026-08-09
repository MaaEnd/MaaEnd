import { createI18n } from 'vue-i18n'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'

export const supportedLocales = [
    'zh-CN',
    'en-US',
] as const
export type SupportedLocale = (typeof supportedLocales)[number]

const localeStorageKey = 'maptracker.locale'

export function resolveLocale(
    savedLocale: string | null,
    browserLanguages: readonly string[],
): SupportedLocale {
    if (supportedLocales.includes(savedLocale as SupportedLocale)) return savedLocale as SupportedLocale
    return browserLanguages[0]?.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US'
}

function initialLocale(): SupportedLocale {
    if (typeof window === 'undefined') return 'en-US'
    try {
        return resolveLocale(window.localStorage.getItem(localeStorageKey), navigator.languages)
    } catch {
        return resolveLocale(null, navigator.languages)
    }
}

const locale = initialLocale()

export const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en-US',
    messages: {
        'zh-CN': zhCN,
        'en-US': enUS,
    },
})

export function setLocale(nextLocale: SupportedLocale) {
    i18n.global.locale.value = nextLocale
    document.documentElement.lang = nextLocale
    try {
        window.localStorage.setItem(localeStorageKey, nextLocale)
    } catch {
        // Persistence may be unavailable; the active locale still changes.
    }
}

if (typeof document !== 'undefined') document.documentElement.lang = locale
