import {depots, destinations, rawJson} from "./model.mjs";

// 普通节点的 zip 与 WithZipline 节点的 zip 默认相反；walk_only / zipline_only 把两个节点
// 统一钉在同一个滑索策略上，避免留下一条已知走不通的路线。zipline_only 的终点在用户选择
// 步行时由 Go 侧直接报错，不会走到这里生成的节点。
function buildRows(routeFileId, id, description, path, routeNode, zipRouteNode, {walkOnly = false, ziplineOnly = false} = {}) {
    const forcedZip = walkOnly ? false : ziplineOnly ? true : null;
    const zipNote = walkOnly ? "仅允许步行" : ziplineOnly ? "仅允许使用滑索" : "允许使用滑索";
    return [
        {
            RouteFileId: routeFileId,
            Node: routeNode,
            Description: `${description}${ziplineOnly ? "，仅允许使用滑索" : ""}（${id}）`,
            ActionParam: rawJson({path, zip: forcedZip ?? false}),
        },
        {
            RouteFileId: routeFileId,
            Node: zipRouteNode,
            Description: `${description}，${zipNote}（${id}）`,
            ActionParam: rawJson({path, zip: forcedZip ?? true}),
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
                      Description: `AutoDelivery 仓储站位修正路线：${depot.name}仓储节点（${depot.id}）`,
                      ActionParam: rawJson({path: depot.retryPath}),
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
                      Description: `AutoDelivery 终点站位修正路线：${destination.name.zh_cn}（${destination.id}）`,
                      ActionParam: rawJson({path: destination.retryPath}),
                  },
              ]
            : []),
    ]),
];
