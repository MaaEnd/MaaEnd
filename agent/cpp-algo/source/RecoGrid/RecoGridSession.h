#pragma once

#include "GridAlignment.h"
#include "RecoGridEngineTypes.h"

#include <map>
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
        int viewportStartRow = 0;
        std::vector<GridScanCell> cells;
        double score = 0.0;
    };

    struct BeamState
    {
        GridHashSnapshot snapshot;
        int viewportStartRow = 0;
        int cols = 0;
        int lastPositiveRowOffset = 0;
        SessionCells cells;
        std::vector<PendingState> pending;
        double score = 0.0;
    };

    GridHashSnapshot snapshot;
    int viewportStartRow = 0;
    int cols = 0;
    int lastPositiveRowOffset = 0;
    SessionCells cells;
    std::vector<PendingState> pending;
    std::vector<BeamState> beams;
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
