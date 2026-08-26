/** Project record/Ziplines.json marks into MapNavigator base-map coordinates. */

function finiteNumber(value) {
    const number = Number(value);
    return Number.isFinite(number) ? number : null;
}

function matchingFrame(config, zoneName) {
    const wanted = String(zoneName || "").toLowerCase();
    return (config && Array.isArray(config.frames) ? config.frames : []).find(
        (frame) => String(frame && frame.zone_name).toLowerCase() === wanted,
    );
}

/**
 * Apply the same world→base calibration as ZiplineFrame::project. This intentionally
 * does not reproduce power, reachability, floor, connectivity, or cost planning.
 * @param {Object} records parsed record/Ziplines.json
 * @param {Object} frameConfig parsed zipline_frames.json
 * @param {string} zoneName geometry/base zone name such as map02base
 * @returns {Array<Object>}
 */
export function projectZiplineRecords(records, frameConfig, zoneName) {
    const frame = matchingFrame(frameConfig, zoneName);
    if (!frame || !Array.isArray(frame.plane) || frame.plane.length !== 6) return [];
    const plane = frame.plane.map(finiteNumber);
    const heightScale = finiteNumber(frame.height_scale);
    const heightOffset = finiteNumber(frame.height_offset);
    if (plane.some((value) => value === null) || heightScale === null || heightOffset === null) return [];
    const accepted = new Set(Array.isArray(frame.template_ids) ? frame.template_ids.map(String) : []);

    const result = [];
    let recordIndex = 0;
    for (const map of records && Array.isArray(records.maps) ? records.maps : []) {
        const mapId = String((map && map.map_id) || "");
        if (frame.map_id && mapId !== String(frame.map_id)) continue;
        for (const mark of map && Array.isArray(map.marks) ? map.marks : []) {
            const templateId = String((mark && mark.template_id) || "");
            if (accepted.size && !accepted.has(templateId)) continue;
            const worldX = finiteNumber(mark && mark.x);
            const worldY = finiteNumber(mark && mark.y);
            const worldZ = finiteNumber(mark && mark.z);
            if ([worldX, worldY, worldZ].some((value) => value === null)) continue;
            result.push({
                measureKey: `record:${recordIndex}`,
                point: [
                    plane[0] * worldX + plane[1] * worldZ + plane[2],
                    plane[3] * worldX + plane[4] * worldZ + plane[5],
                ],
                height: heightScale * worldY + heightOffset,
                world: [worldX, worldY, worldZ],
                mapId,
                levelId: String(mark.level_id || ""),
                templateId,
            });
            recordIndex += 1;
        }
    }
    return result;
}

function ziplineType(frameConfig, templateId) {
    return (frameConfig && Array.isArray(frameConfig.types) ? frameConfig.types : []).find(
        (type) => String(type && type.template_id) === String(templateId || ""),
    );
}

/**
 * Reproduce only the runtime's tower-to-tower geometric span check. Power,
 * reachability, floor snapping, banned edges, and route cost remain runtime-only.
 * @param {Object} lhs
 * @param {Object} rhs
 * @param {Object} frameConfig parsed zipline_frames.json
 * @returns {Object}
 */
export function measureZiplinePair(lhs, rhs, frameConfig) {
    const lhsPoint = lhs && Array.isArray(lhs.point) ? lhs.point : [];
    const rhsPoint = rhs && Array.isArray(rhs.point) ? rhs.point : [];
    const baseDistance =
        lhsPoint.length >= 2 && rhsPoint.length >= 2
            ? Math.hypot(Number(rhsPoint[0]) - Number(lhsPoint[0]), Number(rhsPoint[1]) - Number(lhsPoint[1]))
            : null;
    const lhsWorld = lhs && Array.isArray(lhs.world) ? lhs.world.map(Number) : [];
    const rhsWorld = rhs && Array.isArray(rhs.world) ? rhs.world.map(Number) : [];
    const hasWorld =
        lhsWorld.length >= 3 &&
        rhsWorld.length >= 3 &&
        lhsWorld.slice(0, 3).every(Number.isFinite) &&
        rhsWorld.slice(0, 3).every(Number.isFinite);
    const dx = hasWorld ? rhsWorld[0] - lhsWorld[0] : null;
    const dy = hasWorld ? rhsWorld[1] - lhsWorld[1] : null;
    const dz = hasWorld ? rhsWorld[2] - lhsWorld[2] : null;
    const worldDistance = hasWorld ? Math.sqrt(dx * dx + dy * dy + dz * dz) : null;
    const horizontalDistance = hasWorld ? Math.hypot(dx, dz) : null;
    const heightDelta = hasWorld ? Math.abs(dy) : null;

    const sameTemplate = String((lhs && lhs.templateId) || "") === String((rhs && rhs.templateId) || "");
    const sameLevel = String((lhs && lhs.levelId) || "") === String((rhs && rhs.levelId) || "");
    const type = sameTemplate ? ziplineType(frameConfig, lhs && lhs.templateId) : null;
    const maxSpan = type ? finiteNumber(type.max_span) : null;
    let geometryConnected = null;
    let geometryReason = "缺少世界坐标，无法判断几何连接";
    if (hasWorld && !sameTemplate) {
        geometryConnected = false;
        geometryReason = "滑索架类型不同";
    } else if (hasWorld && !sameLevel) {
        geometryConnected = false;
        geometryReason = "滑索架不在同一 level";
    } else if (hasWorld && !(maxSpan > 0)) {
        geometryConnected = false;
        geometryReason = "该滑索架类型没有有效跨度配置";
    } else if (hasWorld) {
        geometryConnected = worldDistance <= maxSpan;
        geometryReason = geometryConnected
            ? `几何距离不超过 ${maxSpan.toFixed(2)} m 上限`
            : `超出 ${maxSpan.toFixed(2)} m 上限`;
    }

    return {
        baseDistance: Number.isFinite(baseDistance) ? baseDistance : null,
        worldDistance,
        horizontalDistance,
        heightDelta,
        deltaX: dx,
        deltaY: dy,
        deltaZ: dz,
        sameTemplate,
        sameLevel,
        typeName: type ? String(type.name || "") : "",
        maxSpan,
        geometryConnected,
        geometryReason,
    };
}

/** Two-click selection state: first click sets A, second sets B, the next starts over. */
export function nextZiplineMeasurementSelection(current, measureKey) {
    const selection = Array.isArray(current) ? current.filter((key) => typeof key === "string") : [];
    const key = String(measureKey || "");
    if (!key) return selection.slice(0, 2);
    if (selection.length === 1 && selection[0] === key) return [];
    if (selection.length >= 2) return [key];
    return [...selection, key];
}
