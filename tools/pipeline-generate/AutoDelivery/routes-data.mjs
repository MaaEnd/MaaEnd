import {depots, destinations, rawJson} from "./model.mjs";

export function buildRows(routeFileId, id, description, path, routeNode, zipRouteNode, walkOnly = false) {
    return [
        {
            RouteFileId: routeFileId,
            Node: routeNode,
            Description: `${description}（${id}）`,
            ActionParam: rawJson({path, zip: false}),
        },
        {
            RouteFileId: routeFileId,
            Node: zipRouteNode,
            Description: `${description}，${walkOnly ? "仅允许步行" : "允许使用滑索"}（${id}）`,
            ActionParam: rawJson({path, zip: !walkOnly}),
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
            depot.walkOnly,
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
            destination.walkOnly,
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
