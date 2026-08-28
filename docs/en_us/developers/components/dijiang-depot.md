# Dijiang Depot

The Dijiang Depot public Pipeline provides depot screen recognition, navigation, region switching, and item category selection.

## Public Nodes

| Node | Description | Required State |
| --------------------------------------- | ------------------------------------- | -------------------------- |
| `InDijiangDepot` | Checks whether the Dijiang Depot main screen is open | None |
| `SceneEnterMenuBackpackWithDepot` | Opens the Dijiang Depot main screen from any screen | None |
| `DijiangDepotSwitchToValleyIV` | Switches the depot to Valley IV | Depot main or switch screen |
| `DijiangDepotSwitchToWuling` | Switches the depot to Wuling | Depot main or switch screen |

Category nodes require the depot main screen. They complete immediately when the target category is already selected.

| Category | Node |
| ---------------- | ------------------------------------------------- |
| All | `DijiangDepotSelectItemCategoryAll` |
| Ore | `DijiangDepotSelectItemCategoryOre` |
| Plant | `DijiangDepotSelectItemCategoryPlant` |
| Product | `DijiangDepotSelectItemCategoryProduct` |
| Doodad | `DijiangDepotSelectItemCategoryDoodad` |
| Nurturance | `DijiangDepotSelectItemCategoryNurturance` |
| Usable | `DijiangDepotSelectItemCategoryUsable` |
| Producer | `DijiangDepotSelectItemCategoryProducer` |
| Portable Device | `DijiangDepotSelectItemCategoryPortableDevice` |

## Call Order

To switch the depot and category from any screen, call:

1. `SceneEnterMenuBackpackWithDepot`
2. `DijiangDepotSwitchToValleyIV` or `DijiangDepotSwitchToWuling`
3. The required `DijiangDepotSelectItemCategory...` node
