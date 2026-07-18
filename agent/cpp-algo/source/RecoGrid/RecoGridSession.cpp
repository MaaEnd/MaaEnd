#include "RecoGridSession.h"

#include <algorithm>
#include <unordered_set>

namespace recogrid
{

void FinalizeCounts(GridScanResult& result)
{
    result.sessionTotalCells = static_cast<int>(result.cells.size());
    result.sessionRows = 0;
    result.knownCells = 0;
    result.unknownCells = 0;
    for (const GridScanCell& cell : result.cells) {
        result.sessionRows = std::max(result.sessionRows, cell.row + 1);
        if (cell.matched) {
            ++result.knownCells;
        }
        else {
            ++result.unknownCells;
        }
    }
}

std::vector<GridScanCell> ToSortedCells(const SessionCells& cells)
{
    std::vector<GridScanCell> output;
    output.reserve(cells.size());
    for (const auto& [_, cell] : cells) {
        output.push_back(cell);
    }
    return output;
}

void HideSessionCells(SessionCells& cells)
{
    for (auto& [_, cell] : cells) {
        cell.visible = false;
        cell.screenCell = {};
    }
}

bool ShouldReplaceCell(const GridScanCell& current, const GridScanCell& candidate)
{
    if (candidate.matched && !current.matched) {
        return true;
    }
    if (!candidate.matched) {
        return false;
    }
    if (candidate.score != current.score) {
        return candidate.score > current.score;
    }
    if (candidate.templateScore != current.templateScore) {
        return candidate.templateScore > current.templateScore;
    }
    if (candidate.phashDistance != current.phashDistance) {
        return candidate.phashDistance < current.phashDistance;
    }
    return candidate.templateId < current.templateId;
}

int UpsertSessionCell(SessionCells& cells, const GridScanCell& visibleCell)
{
    const auto key = std::make_pair(visibleCell.row, visibleCell.col);
    auto iter = cells.find(key);
    if (iter == cells.end()) {
        cells.emplace(key, visibleCell);
        return 1;
    }

    GridScanCell& target = iter->second;
    if (ShouldReplaceCell(target, visibleCell)) {
        target = visibleCell;
        return 0;
    }

    target.cellIndex = visibleCell.cellIndex;
    target.screenCell = visibleCell.screenCell;
    target.visible = true;
    return 0;
}

int UpsertSessionCells(SessionCells& cells, const std::vector<GridScanCell>& visibleCells)
{
    int inserted = 0;
    for (const GridScanCell& cell : visibleCells) {
        inserted += UpsertSessionCell(cells, cell);
    }
    return inserted;
}

int MaxVisibleRow(const std::vector<GridScanCell>& cells)
{
    int maxRow = -1;
    for (const GridScanCell& cell : cells) {
        maxRow = std::max(maxRow, cell.row);
    }
    return maxRow;
}

std::unordered_set<int> VisibleColsInRow(const std::vector<GridScanCell>& cells, int row)
{
    std::unordered_set<int> cols;
    for (const GridScanCell& cell : cells) {
        if (cell.row == row) {
            cols.insert(cell.col);
        }
    }
    return cols;
}

bool HasTrailingPartialRow(const std::vector<GridScanCell>& cells, int cols)
{
    if (cols <= 0) {
        return false;
    }

    const int lastRow = MaxVisibleRow(cells);
    if (lastRow < 0) {
        return false;
    }

    const std::unordered_set<int> visibleCols = VisibleColsInRow(cells, lastRow);
    return !visibleCols.empty() && static_cast<int>(visibleCols.size()) < cols;
}

int CountNewVisibleSessionKeys(const SessionCells& sessionCells, const std::vector<GridScanCell>& visibleCells)
{
    int count = 0;
    for (const GridScanCell& cell : visibleCells) {
        if (sessionCells.find(std::make_pair(cell.row, cell.col)) == sessionCells.end()) {
            ++count;
        }
    }
    return count;
}

} // namespace recogrid
