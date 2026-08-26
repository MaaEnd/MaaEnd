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
        }
    }
    return result;
}
