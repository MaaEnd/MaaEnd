import {depots, destinations, rawJson} from "./model.mjs";

// 普通节点的 zip 与 WithZipline 节点的 zip 始终取各自节点的语义：普通节点仅步行、WithZipline
// 节点允许滑索。walk_only / zipline_only 是一条路线的滑索策略，写在运行时映射（catalog.json）
// 里由 Go 分发时处理，不在生成节点上改写 zip，免得节点名与参数互相矛盾。
function buildRows(routeFileId, id, description, path, routeNode, zipRouteNode, {walkOnly = false, ziplineOnly = false} = {}) {
    const zipNote = walkOnly ? "仅允许步行" : ziplineOnly ? "仅允许使用滑索" : "允许使用滑索";
    return [
        {
            RouteFileId: routeFileId,
            Node: routeNode,
            Description: `${description}${ziplineOnly ? "，仅允许使用滑索" : ""}(${id})`,
            ActionParam: rawJson({path, zip: false, heading_source: "camera"}),
        },
        {
            RouteFileId: routeFileId,
            Node: zipRouteNode,
            Description: `${description}，${zipNote}(${id})`,
            ActionParam: rawJson({path, zip: true, heading_source: "camera"}),
        },
    ];
}

export default [
    ...depots.flatMap((depot) => [
        ...buildRows(
            depot.routeFileId,
            depot.id,
            `AutoDelivery 仓储路线：前往${depot.name}仓储节点`,
            depot.path,
            depot.routeNode,
            depot.zipRouteNode,
            depot,
        ),
        ...(depot.retryRouteNode
            ? [
                  {
                      RouteFileId: depot.routeFileId,
                      Node: depot.retryRouteNode,
                      Description: `AutoDelivery 仓储站位修正路线：${depot.name}仓储节点(${depot.id})`,
                      ActionParam: rawJson({path: depot.retryPath, heading_source: "camera"}),
                  },
              ]
            : []),
    ]),
    ...destinations.flatMap((destination) => [
        ...buildRows(
            destination.routeFileId,
            destination.id,
            `AutoDelivery 终点路线：从${destination.depotName}仓储节点前往${destination.name.zh_cn}`,
            destination.path,
            destination.routeNode,
            destination.zipRouteNode,
            destination,
        ),
        ...(destination.retryRouteNode
            ? [
                  {
                      RouteFileId: destination.routeFileId,
                      Node: destination.retryRouteNode,
                      Description: `AutoDelivery 终点站位修正路线：${destination.name.zh_cn}(${destination.id})`,
                      ActionParam: rawJson({path: destination.retryPath, heading_source: "camera"}),
                  },
              ]
            : []),
    ]),
];
