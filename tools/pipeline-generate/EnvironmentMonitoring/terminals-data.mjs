import rows, {kiteStationData, MONITORING_TERMINAL_IDS} from "./data.mjs";

function toPascalCase(str) {
    const cleaned = String(str || "")
        .replace(/[^a-zA-Z0-9]+/g, " ")
        .trim();
    if (!cleaned) return "";
    return cleaned
        .split(/\s+/)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join("");
}

function buildTerminalId(terminalId) {
    const enName = kiteStationData?.[terminalId]?.level?.name?.["en-US"];
    return toPascalCase(enName || terminalId) || terminalId;
}

function buildTerminalName(terminalId) {
    return kiteStationData?.[terminalId]?.level?.name?.["zh-CN"] || terminalId;
}

// 当一个任务名是另一个任务名的子串时（如 "蓄水源石虫" ⊂ "充满活力的蓄水源石虫"），
// 必须先尝试匹配更具体的版本，否则 OCR 会被短名吞掉。
// 在保持 entrustIdx 原序的前提下，把更具体的任务上浮到其子串版本之前。
function reorderBySpecificity(items) {
    const arr = [...items];
    let changed = true;
    while (changed) {
        changed = false;
        outer: for (let i = 0; i < arr.length; i++) {
            for (let j = i + 1; j < arr.length; j++) {
                if (arr[i].Name !== arr[j].Name && arr[j].Name.includes(arr[i].Name)) {
                    const [moved] = arr.splice(j, 1);
                    arr.splice(i, 0, moved);
                    changed = true;
                    break outer;
                }
            }
        }
    }
    return arr;
}

function buildTerminalNext(station) {
    const stationRows = rows.filter((row) => row.Station === station);
    return reorderBySpecificity(stationRows)
        .map((row) => `[JumpBack]${row.Id}Job`)
        .concat("EnvironmentMonitoringFinish");
}

export default MONITORING_TERMINAL_IDS.map((terminalId) => {
    const Id = buildTerminalId(terminalId);
    return {
        Id,
        Name: buildTerminalName(terminalId),
        Next: buildTerminalNext(Id),
    };
});
