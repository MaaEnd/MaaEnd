import {buildNodeId, destinations} from "./model.mjs";

export const recycleBins = destinations.filter((destination) => destination.kind === "recycle_bin");

function candidateNode(destination) {
    return `AutoDeliveryFindRecycleBin${buildNodeId(destination.id)}`;
}

export function groupAmbiguousRecycleBins(sourceDestinations) {
    const recycleBinsByArea = new Map();
    for (const destination of sourceDestinations) {
        if (destination.kind !== "recycle_bin") {
            continue;
        }
        const key = `${destination.map}:${destination.areaId}`;
        const areaDestinations = recycleBinsByArea.get(key) ?? [];
        areaDestinations.push(destination);
        recycleBinsByArea.set(key, areaDestinations);
    }
    return [...recycleBinsByArea.values()].filter((areaDestinations) => areaDestinations.length > 1);
}

export const ambiguousRecycleBinGroups = groupAmbiguousRecycleBins(recycleBins);
export const ambiguousRecycleBins = ambiguousRecycleBinGroups.flat();

export const recycleBinResolveNodes = ambiguousRecycleBins.map((destination) => ({
    node: candidateNode(destination),
    destinationId: destination.id,
}));

export const recycleBinAreaRows = ambiguousRecycleBinGroups.map((areaDestinations) => {
    const [sample] = areaDestinations;
    return {
        AreaId: sample.areaId,
        AreaName: sample.area.zh_cn,
        CandidateNodes: areaDestinations.map(candidateNode),
    };
});

export const recycleBinCandidateRows = ambiguousRecycleBins.map((destination) => ({
    AreaName: destination.area.zh_cn,
    DestinationId: destination.id,
    DestinationNodeId: buildNodeId(destination.id),
    DestinationMapAt: destination.mapAt,
    MapZone: destination.mapZone,
}));

export default recycleBinAreaRows;
