import {readFileSync} from "node:fs";

const catalogSource = JSON.parse(readFileSync(new URL("../data/delivery_destinations.json", import.meta.url), "utf8"));
const routeSource = JSON.parse(readFileSync(new URL("./routes.json", import.meta.url), "utf8"));

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

function assertUnique(items, keyOf, label) {
    const seen = new Set();
    for (const item of items) {
        const key = keyOf(item);
        if (seen.has(key)) {
            throw new Error(`[AutoDelivery] ${label} 存在重复项：${key}`);
        }
        seen.add(key);
    }
}

function buildNodeId(sourceId) {
    return sourceId
        .split(/[^A-Za-z0-9]+/)
        .filter(Boolean)
        .map((part) => `${part[0].toUpperCase()}${part.slice(1)}`)
        .join("");
}

function buildRouteNode(kind, sourceId, zip = false) {
    return `AutoDeliveryRoute${kind}${buildNodeId(sourceId)}${zip ? "WithZipline" : ""}`;
}

function buildNavmeshPath(source, label) {
    if (!Number.isFinite(source.u) || !Number.isFinite(source.v) || source.u < 0 || source.v < 0) {
        throw new TypeError(`[AutoDelivery] ${label} 的 u/v 坐标无效`);
    }
    return [
        {
            action: "NAVMESH",
            target: [
                source.u,
                source.v,
            ],
        },
    ];
}

const depotOverrideItems = assertArray(routeSource.depots, "routes.depots").map((item, index) => {
    const sourceId = assertNonEmptyString(item.source_id, `routes.depots[${index}].source_id`);
    return [
        sourceId,
        item,
    ];
});
const destinationOverrideItems = assertArray(routeSource.destinations, "routes.destinations").map((item, index) => {
    const sourceId = assertNonEmptyString(item.source_id, `routes.destinations[${index}].source_id`);
    return [
        sourceId,
        item,
    ];
});
assertUnique(depotOverrideItems, ([id]) => id, "仓储路线覆盖");
assertUnique(destinationOverrideItems, ([id]) => id, "终点路线覆盖");

const depotOverrides = new Map(depotOverrideItems);
const destinationOverrides = new Map(destinationOverrideItems);

export const depots = assertArray(catalogSource.depots, "delivery_destinations.depots").map((source, index) => {
    const id = assertNonEmptyString(source.id, `depots[${index}].id`);
    const override = depotOverrides.get(id);
    const path = override?.path?.length ? override.path : buildNavmeshPath(source, `仓储 ${id}`);
    return {
        id,
        name: assertNonEmptyString(source.name?.zh_cn, `depots[${index}].name.zh_cn`),
        map: assertNonEmptyString(source.map, `depots[${index}].map`),
        path,
        retryPath: override?.retry_path ?? [],
        departurePath: override?.departure_path ?? [],
        routeNode: buildRouteNode("Depot", id),
        zipRouteNode: buildRouteNode("Depot", id, true),
        retryRouteNode: override?.retry_path?.length ? buildRouteNode("DepotRetry", id) : "",
    };
});
assertUnique(depots, (item) => item.id, "仓储 ID");

const depotById = new Map(
    depots.map((item) => [
        item.id,
        item,
    ]),
);
for (const id of depotOverrides.keys()) {
    if (!depotById.has(id)) {
        throw new Error(`[AutoDelivery] 仓储路线覆盖 ${id} 未匹配到生成目录`);
    }
}

export const destinations = assertArray(catalogSource.destinations, "delivery_destinations.destinations")
    .map((source, index) => {
        const id = assertNonEmptyString(source.id, `destinations[${index}].id`);
        if (source.kind !== "npc" && source.kind !== "recycle_bin") {
            throw new Error(`[AutoDelivery] 终点 ${id} 的 kind 无效：${source.kind}`);
        }
        const depot = depotById.get(source.depot_id);
        if (!depot) {
            throw new Error(`[AutoDelivery] 终点 ${id} 引用了未知仓储 ${source.depot_id}`);
        }
        const override = destinationOverrides.get(id);
        const ownPath = override?.path?.length ? override.path : buildNavmeshPath(source, `终点 ${id}`);
        return {
            id,
            kind: source.kind,
            depotId: source.depot_id,
            depotName: depot.name,
            name: source.name,
            mission: source.mission,
            area: source.area,
            path: [
                ...depot.departurePath,
                ...ownPath,
            ],
            routeNode: buildRouteNode("Destination", id),
            zipRouteNode: buildRouteNode("Destination", id, true),
        };
    })
    .sort((left, right) => left.id.localeCompare(right.id));
assertUnique(destinations, (item) => item.id, "终点 ID");

const destinationById = new Map(
    destinations.map((item) => [
        item.id,
        item,
    ]),
);
for (const id of destinationOverrides.keys()) {
    if (!destinationById.has(id)) {
        throw new Error(`[AutoDelivery] 终点路线覆盖 ${id} 未匹配到生成目录`);
    }
}

export const runtimeCatalog = {
    depots: depots.map((item) => ({
        id: item.id,
        map: item.map,
        route_node: item.routeNode,
        zip_route_node: item.zipRouteNode,
        ...(item.retryRouteNode ? {retry_route_node: item.retryRouteNode} : {}),
    })),
    destinations: destinations.map((item) => ({
        id: item.id,
        kind: item.kind,
        depot_id: item.depotId,
        name: item.name,
        mission: item.mission,
        area: item.area,
        route_node: item.routeNode,
        zip_route_node: item.zipRouteNode,
    })),
};

export function rawJson(value) {
    return {value, raw: JSON.stringify(value, null, 4)};
}
