import {normalizeZoneId} from "./model.js";

/**
 * Select the offline preview start and the authored targets sent to MapNavigator.
 * A manual start is preview-only, so every authored point remains a target. Without
 * one, the first authored point represents the already-reached start for compatibility.
 *
 * @param {Object[]} points current authored segment
 * @param {?{position:number[],positionZone:string}} explicitStart preview start in its original zone frame
 * @returns {{ok:true,position:number[],positionZone:string,targets:Object[],explicit:boolean}|{ok:false,error:string}}
 */
export function buildEditPreviewPlan(points, explicitStart = null) {
    const authored = Array.isArray(points) ? points : [];
    if (explicitStart) {
        if (authored.length < 1) {
            return {ok: false, error: "请先添加至少一个目标路点再规划。"};
        }
        const positionZone = normalizeZoneId(explicitStart.positionZone);
        const position = Array.isArray(explicitStart.position) ? explicitStart.position.slice(0, 2) : [];
        if (position.length < 2 || !position.every(Number.isFinite) || !positionZone) {
            return {ok: false, error: "手动规划起点缺少有效坐标或区域。"};
        }
        return {
            ok: true,
            position,
            positionZone,
            targets: authored.slice(),
            explicit: true,
        };
    }

    if (authored.length < 2) {
        return {ok: false, error: "当前片段至少需要两个路点，或先在地图上设置规划起点。"};
    }
    const start = authored[0];
    const positionZone = normalizeZoneId(start.target_tier || start.zone);
    if (!positionZone) {
        return {ok: false, error: "当前片段缺少起点区域，无法交给运行时规划。"};
    }
    return {
        ok: true,
        position: [start.x, start.y],
        positionZone,
        targets: authored.slice(1),
        explicit: false,
    };
}
