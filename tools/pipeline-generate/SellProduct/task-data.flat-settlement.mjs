/**
 * task-template.jsonc + data.mjs（扁平行，多 case 来自 buildItemCases）
 *
 * node generate-task.mjs --template ./task-template.jsonc --data ./task-data.flat-settlement.mjs
 */
import { settlementFlatRows, taskFromSettlementConfig } from "./data.mjs";

export default settlementFlatRows;

export const config = {
    ...taskFromSettlementConfig,
    outputFile: "SellProduct.task-template.json",
};
