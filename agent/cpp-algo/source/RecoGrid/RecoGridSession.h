#pragma once

#include "GridAlignment.h"
#include "RecoGridEngineTypes.h"

#include <map>
#include <optional>
#include <string>
#include <utility>
#include <vector>

namespace recogrid
{

using SessionCells = std::map<std::pair<int, int>, GridScanCell>;

struct SessionState
{
    struct PendingState
    {
        GridHashSnapshot snapshot;
    };

    GridHashSnapshot snapshot;
    int viewportStartRow = 0;
    int cols = 0;
    int lastPositiveRowOffset = 0;
    int endConfirmations = 0;
    int lockedRowHeight = 0;
    int lockedColWidth = 0;
    SessionCells cells;
    std::optional<PendingState> pending;
};

void FinalizeCounts(GridScanResult& result);
std::vector<GridScanCell> ToSortedCells(const SessionCells& cells);
void HideSessionCells(SessionCells& cells);
int UpsertSessionCell(SessionCells& cells, const GridScanCell& visibleCell);
int UpsertSessionCells(SessionCells& cells, const std::vector<GridScanCell>& visibleCells);
int MaxVisibleRow(const std::vector<GridScanCell>& cells);
bool HasTrailingPartialRow(const std::vector<GridScanCell>& cells, int cols);
int CountNewVisibleSessionKeys(const SessionCells& sessionCells, const std::vector<GridScanCell>& visibleCells);

} // namespace recogrid
