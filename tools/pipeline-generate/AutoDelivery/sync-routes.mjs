import {readFileSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";

const catalogPath = new URL("../data/delivery_destinations.json", import.meta.url);
const routesPath = new URL("./routes.json", import.meta.url);

const DEPOT_METADATA_KEYS = new Set([
    "source_id",
    "name",
]);
const DESTINATION_METADATA_KEYS = new Set([
    "source_id",
    "name",
    "depot_id",
]);

function assertArray(value, label) {
    if (!Array.isArray(value)) {
        throw new TypeError(`[AutoDelivery] ${label} 必须是数组`);
    }
    return value;
}

function assertNonEmptyString(value, label) {
    if (typeof value !== "string" || value.trim() === "") {
        throw new TypeError(`[AutoDelivery] ${label} 必须是非空字符串`);
    }
    return value;
}

function buildRouteIndex(routes, label) {
    const routeById = new Map();
    for (const [
        index,
        route,
    ] of routes.entries()) {
        const sourceId = assertNonEmptyString(route.source_id, `${label}[${index}].source_id`);
        if (routeById.has(sourceId)) {
            throw new Error(`[AutoDelivery] ${label} 存在重复项：${sourceId}`);
        }
        routeById.set(sourceId, route);
    }
    return routeById;
}

function preserveRouteFields(synced, route, metadataKeys) {
    if (!route) {
        return synced;
    }
    for (const [
        key,
        value,
    ] of Object.entries(route)) {
        if (!metadataKeys.has(key)) {
            synced[key] = value;
        }
    }
    return synced;
}

function syncItems(sources, routes, label, buildMetadata, metadataKeys) {
    const routeById = buildRouteIndex(routes, label);
    const synced = sources.map((source, index) => {
        const sourceId = assertNonEmptyString(source.id, `${label}数据[${index}].id`);
        const metadata = buildMetadata(source, index);
        const route = routeById.get(sourceId);
        routeById.delete(sourceId);
        return preserveRouteFields(metadata, route, metadataKeys);
    });

    for (const route of routeById.values()) {
        console.warn(`[AutoDelivery] ${label}条目 ${route.source_id} 未匹配到当前游戏数据，已保留等待人工处理。`);
        synced.push(route);
    }

    return synced.sort((left, right) => left.source_id.localeCompare(right.source_id));
}

export function buildSyncedRouteConfig(catalog, routes) {
    const depots = syncItems(
        assertArray(catalog.depots, "delivery_destinations.depots"),
        assertArray(routes.depots, "routes.depots"),
        "仓储路线",
        (source, index) => ({
            source_id: source.id,
            name: assertNonEmptyString(source.name?.zh_cn, `depots[${index}].name.zh_cn`),
        }),
        DEPOT_METADATA_KEYS,
    );
    const destinations = syncItems(
        assertArray(catalog.destinations, "delivery_destinations.destinations"),
        assertArray(routes.destinations, "routes.destinations"),
        "终点路线",
        (source, index) => ({
            source_id: source.id,
            name: assertNonEmptyString(source.name?.zh_cn, `destinations[${index}].name.zh_cn`),
            depot_id: assertNonEmptyString(source.depot_id, `destinations[${index}].depot_id`),
        }),
        DESTINATION_METADATA_KEYS,
    );
    return {
        destinations,
        depots,
    };
}

export function syncRouteConfig() {
    const originalText = readFileSync(routesPath, "utf8");
    const catalog = JSON.parse(readFileSync(catalogPath, "utf8"));
    const routes = JSON.parse(originalText);
    const syncedText = `${JSON.stringify(buildSyncedRouteConfig(catalog, routes), null, 4)}\n`;

    if (syncedText === originalText.replace(/\r\n/g, "\n")) {
        console.log("[AutoDelivery] routes.json 元数据未变化");
        return;
    }
    writeFileSync(routesPath, syncedText, "utf8");
    console.log("[AutoDelivery] 已同步 routes.json 的 source_id/name/depot_id，并保留人工路线字段");
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
    syncRouteConfig();
}
