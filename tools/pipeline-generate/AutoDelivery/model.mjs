import {readFileSync} from "node:fs";

import {BASE_NAV_ZONE_IMAGE_PARTS} from "../../MapNavigator/web/static/js/model.js";

const catalogSource = JSON.parse(readFileSync(new URL("../data/delivery_destinations.json", import.meta.url), "utf8"));
const routeSource = JSON.parse(readFileSync(new URL("./routes.json", import.meta.url), "utf8"));

const APPROACH_DISTANCE_METERS = 8;
const COORDINATE_PRECISION = 3;

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

function readWalkOnly(value, label) {
    if (value === undefined) {
        return false;
    }
    if (typeof value !== "boolean") {
        throw new TypeError(`[AutoDelivery] ${label}.walk_only 必须是布尔值`);
    }
    return value;
}

// 数据源的 yaw 是游戏内实测的实体朝向，部分 NPC 面向墙或缺失朝向（缺省 0），
// 自动接近点会因此落进墙体。yaw 覆盖让维护者按实际可站位方向修正接近点，不必整条重写路线。
function readYawOverride(value, label) {
    if (value === undefined) {
        return null;
    }
    if (!Number.isFinite(value)) {
        throw new TypeError(`[AutoDelivery] ${label}.yaw 必须是有限数值`);
    }
    return ((value % 360) + 360) % 360;
}

// 数据源的 u/v 由游戏世界坐标投影得来，落点可能与实际可交互位置差几米（例如被高架步道
// 挡住、终点压在另一张可走面上）。offset 用底图像素偏移微调落点，接近点跟着一起移动。
function readOffset(value, label) {
    if (value === undefined) {
        return null;
    }
    if (!Array.isArray(value) || value.length !== 2 || !value.every((item) => Number.isFinite(item))) {
        throw new TypeError(`[AutoDelivery] ${label}.offset 必须是 [du, dv] 两个有限数值`);
    }
    if (value[0] === 0 && value[1] === 0) {
        throw new Error(`[AutoDelivery] ${label}.offset 是全零偏移，没有作用对象`);
    }
    return value;
}

function assertAutoGenerationOverrideUsed(override, label) {
    const knobs = [
        "yaw",
        "offset",
    ].filter((key) => override?.[key] !== undefined);
    if (knobs.length === 0) {
        return;
    }
    if (override?.path?.length && override?.retry_path?.length) {
        throw new Error(
            `[AutoDelivery] ${label} 同时覆盖了 path 与 retry_path，${knobs.join(" / ")} 没有可作用的自动生成坐标`,
        );
    }
}

function readMap(source, label) {
    const mapId = assertNonEmptyString(source.map, `${label}.map`);
    const map = catalogSource.maps?.[mapId];
    if (!map) {
        throw new Error(`[AutoDelivery] ${label} 引用了未知地图 ${mapId}`);
    }
    return map;
}

function shiftSource(source, offset, label) {
    if (offset === null) {
        return source;
    }
    const map = readMap(source, label);
    const [width, height] = map.size ?? [];
    const u = roundCoordinate(source.u + offset[0]);
    const v = roundCoordinate(source.v + offset[1]);
    if (!Number.isFinite(width) || !Number.isFinite(height) || u < 0 || u >= width || v < 0 || v >= height) {
        throw new RangeError(
            `[AutoDelivery] ${label} 的 offset ${JSON.stringify(offset)} 把落点移出底图：u=${u} v=${v}（图 ${width}x${height}）`,
        );
    }
    return {
        ...source,
        u,
        v,
    };
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

export function buildNodeId(sourceId) {
    return sourceId
        .split(/[^A-Za-z0-9]+/)
        .filter(Boolean)
        .map((part) => `${part[0].toUpperCase()}${part.slice(1)}`)
        .join("");
}

function buildAreaId(area, label) {
    const english = assertNonEmptyString(area?.en_us, `${label}.area.en_us`);
    const id = english.replace(/[^A-Za-z0-9]/g, "");
    if (id === "") {
        throw new TypeError(`[AutoDelivery] ${label} 的 area.en_us 无法生成区域 ID`);
    }
    return id;
}

function buildRouteFileId(name, label) {
    const id = buildNodeId(assertNonEmptyString(name?.en_us, `${label}.name.en_us`));
    if (id === "") {
        throw new TypeError(`[AutoDelivery] ${label} 的 name.en_us 无法生成路线文件 ID`);
    }
    return id;
}

function readMapLocatorEntry(map, label) {
    const baseNavZone = assertNonEmptyString(catalogSource.maps?.[map]?.zone, `${label}.maps.${map}.zone`);
    const [
        resourceType,
        zone,
        imageFile,
    ] = BASE_NAV_ZONE_IMAGE_PARTS[baseNavZone] ?? [];
    if (resourceType !== "MapLocator" || !zone || !imageFile) {
        throw new Error(`[AutoDelivery] ${label} 的 BaseNav 地区 ${baseNavZone} 无法对应 MapLocator 地区`);
    }
    return {zone, imageFile};
}

function buildMapZone(map, label) {
    return readMapLocatorEntry(map, label).zone;
}

// 路线首点由生成器统一声明 MapLocator 区域名，routes.json 只维护路点。
// 定位器在起步冷启动时会把首点的 zone_id 当作期望区域，只接受落在该区域内的 YOLO 结果。

export function buildLocatorZoneId(map, label) {
    const {zone, imageFile} = readMapLocatorEntry(map, label);
    const stem = imageFile.replace(/\.png$/i, "");
    return stem === "Base" ? `${zone}_Base` : stem;
}

function withZoneDeclaration(path, zoneId, label) {
    const [first] = path;
    const declared = first && !Array.isArray(first) && first.action === "ZONE" ? first : null;
    if (declared) {
        const region = zoneId.split("_")[0];
        if (typeof declared.zone_id !== "string" || !declared.zone_id.startsWith(`${region}_`)) {
            throw new Error(
                `[AutoDelivery] ${label} 的 ZONE 区域 ${declared.zone_id} 与预期的 ${zoneId} 不属于同一区域`,
            );
        }
    }
    return [
        {
            action: "ZONE",
            zone_id: zoneId,
        },
        ...(declared ? path.slice(1) : path),
    ];
}

function buildRouteNode(kind, sourceId, zip = false) {
    return `AutoDeliveryRoute${kind}${buildNodeId(sourceId)}${zip ? "WithZipline" : ""}`;
}

function roundCoordinate(value) {
    return Number(value.toFixed(COORDINATE_PRECISION));
}

export function buildYawApproachTarget(source, map, label, yawOverride = null) {
    if (!Number.isFinite(source.u) || !Number.isFinite(source.v) || source.u < 0 || source.v < 0) {
        throw new TypeError(`[AutoDelivery] ${label} 的 u/v 坐标无效`);
    }

    const yaw = yawOverride ?? source.yaw;
    if (!Number.isFinite(yaw)) {
        throw new TypeError(`[AutoDelivery] ${label} 的 yaw 朝向无效`);
    }
    if (!Number.isFinite(map?.sx) || map.sx <= 0 || !Number.isFinite(map?.sy) || map.sy <= 0) {
        throw new TypeError(`[AutoDelivery] ${label} 的地图 sx/sy 比例无效`);
    }

    const radians = (yaw * Math.PI) / 180;
    return [
        roundCoordinate(source.u + APPROACH_DISTANCE_METERS * map.sx * Math.sin(radians)),
        roundCoordinate(source.v - APPROACH_DISTANCE_METERS * map.sy * Math.cos(radians)),
    ];
}

export function buildNavmeshPath(source, label, {withApproachPoint = false, yaw = null, offset = null} = {}) {
    if (!Number.isFinite(source.u) || !Number.isFinite(source.v) || source.u < 0 || source.v < 0) {
        throw new TypeError(`[AutoDelivery] ${label} 的 u/v 坐标无效`);
    }
    if (!Number.isFinite(source.y)) {
        throw new TypeError(`[AutoDelivery] ${label} 的 y 世界高度无效`);
    }

    // 底图是二维的，同一格可能压着上下多张可走面；自动生成的点都要声明自己落在哪张面上，
    // 否则寻路会在重叠面里任选一张停下，且二维到达判定照样通过，属于静默走错层。
    // 接近点与终点同层，共用实体自身的世界高度。
    const shifted = shiftSource(source, offset, label);
    const deckY = roundCoordinate(shifted.y);
    const destination = {
        action: "NAVMESH",
        target: [
            shifted.u,
            shifted.v,
        ],
        target_deck_y: deckY,
    };
    if (!withApproachPoint) {
        return [destination];
    }

    const map = readMap(shifted, label);
    return [
        {
            action: "NAVMESH",
            target: buildYawApproachTarget(shifted, map, label, yaw),
            target_deck_y: deckY,
            required: true,
        },
        destination,
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
    assertAutoGenerationOverrideUsed(override, `仓储 ${id}`);
    const walkOnly = readWalkOnly(override?.walk_only, `仓储 ${id}`);
    const defaultPath = buildNavmeshPath(source, `仓储 ${id}`, {
        withApproachPoint: true,
        yaw: readYawOverride(override?.yaw, `仓储 ${id}`),
        offset: readOffset(override?.offset, `仓储 ${id}`),
    });
    const zoneId = buildLocatorZoneId(source.map, `仓储 ${id}`);
    const path = withZoneDeclaration(override?.path?.length ? override.path : defaultPath, zoneId, `仓储 ${id}`);
    const retryPath = withZoneDeclaration(
        override?.retry_path?.length ? override.retry_path : defaultPath,
        zoneId,
        `仓储重试 ${id}`,
    );
    return {
        id,
        name: assertNonEmptyString(source.name?.zh_cn, `depots[${index}].name.zh_cn`),
        names: source.name,
        routeFileId: buildRouteFileId(source.name, `depots[${index}]`),
        map: assertNonEmptyString(source.map, `depots[${index}].map`),
        path,
        retryPath,
        departurePath: override?.departure_path ?? [],
        walkOnly,
        routeNode: buildRouteNode("Depot", id),
        zipRouteNode: buildRouteNode("Depot", id, true),
        retryRouteNode: buildRouteNode("DepotRetry", id),
    };
});
assertUnique(depots, (item) => item.id, "仓储 ID");
assertUnique(depots, (item) => item.routeFileId, "仓储路线文件 ID");

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
        if (source.kind === "recycle_bin" && (!Number.isInteger(source.serial_id) || source.serial_id <= 0)) {
            throw new Error(`[AutoDelivery] 资源回收站终点 ${id} 的 serial_id 无效：${source.serial_id}`);
        }
        const depot = depotById.get(source.depot_id);
        if (!depot) {
            throw new Error(`[AutoDelivery] 终点 ${id} 引用了未知仓储 ${source.depot_id}`);
        }
        const override = destinationOverrides.get(id);
        assertAutoGenerationOverrideUsed(override, `终点 ${id}`);
        const walkOnly = readWalkOnly(override?.walk_only, `终点 ${id}`);
        const yaw = readYawOverride(override?.yaw, `终点 ${id}`);
        const offset = readOffset(override?.offset, `终点 ${id}`);
        const withApproachPoint = source.kind === "recycle_bin";
        const defaultPath = buildNavmeshPath(source, `终点 ${id}`, {
            withApproachPoint,
            yaw,
            offset,
        });
        const ownPath = override?.path?.length ? override.path : defaultPath;
        const defaultRetryPath = buildNavmeshPath(source, `终点重试 ${id}`, {
            withApproachPoint: true,
            yaw,
            offset,
        });
        const retryPath = withZoneDeclaration(
            override?.retry_path?.length ? override.retry_path : defaultRetryPath,
            buildLocatorZoneId(depot.map, `终点 ${id}`),
            `终点重试 ${id}`,
        );
        // departurePath 会拼接到终点主路线之前，首点 ZONE 由 withZoneDeclaration 统一归一化。
        const path = withZoneDeclaration(
            [
                ...depot.departurePath,
                ...ownPath,
            ],
            buildLocatorZoneId(depot.map, `终点 ${id}`),
            `终点 ${id}`,
        );
        return {
            id,
            kind: source.kind,
            serialId: source.kind === "recycle_bin" ? source.serial_id : null,
            areaId: buildAreaId(source.area, `destinations[${index}]`),
            map: depot.map,
            mapZone: buildMapZone(depot.map, `终点 ${id}`),
            depotId: source.depot_id,
            depotName: depot.name,
            routeFileId: depot.routeFileId,
            name: source.name,
            mission: source.mission,
            area: source.area,
            mapAt: [
                source.u,
                source.v,
            ],
            path,
            retryPath,
            walkOnly,
            routeNode: buildRouteNode("Destination", id),
            zipRouteNode: buildRouteNode("Destination", id, true),
            retryRouteNode: buildRouteNode("DestinationRetry", id),
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
        name: item.names,
        map: item.map,
        route_node: item.routeNode,
        zip_route_node: item.zipRouteNode,
        ...(item.retryRouteNode ? {retry_route_node: item.retryRouteNode} : {}),
    })),
    destinations: destinations.map((item) => ({
        id: item.id,
        kind: item.kind,
        ...(item.serialId === null ? {} : {serial_id: item.serialId}),
        depot_id: item.depotId,
        name: item.name,
        mission: item.mission,
        area: item.area,
        route_node: item.routeNode,
        zip_route_node: item.zipRouteNode,
        ...(item.retryRouteNode ? {retry_route_node: item.retryRouteNode} : {}),
    })),
};

export function rawJson(value) {
    return {value, raw: JSON.stringify(value, null, 4)};
}
