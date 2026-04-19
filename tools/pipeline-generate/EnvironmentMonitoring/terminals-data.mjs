import { createRequire } from "module";
import rows, { MONITORING_TERMINAL_IDS } from "./data.mjs";

const require = createRequire(import.meta.url);
const kiteStationData = require("./kite_station.json");

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

function buildTerminalNext(station) {
    return rows
        .filter((row) => row.Station === station)
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
