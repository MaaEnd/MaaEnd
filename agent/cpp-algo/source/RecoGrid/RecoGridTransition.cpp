#include "RecoGridTransition.h"

#include "RecoGridPlacement.h"

#include <algorithm>
#include <utility>

namespace recogrid
{
namespace
{

constexpr int kRequiredEndConfirmations = 2;

void KeepSessionResult(GridScanResult& result, const SessionState& session, bool reachedEnd, std::string message)
{
    result.success = true;
    result.message = std::move(message);
    result.reachedEnd = reachedEnd;
    result.sessionCols = session.cols;
    result.previousViewportStartRow = session.viewportStartRow;
    result.currentViewportStartRow = session.viewportStartRow;
    result.endConfirmations = session.endConfirmations;
    result.cells = ToSortedCells(session.cells);
    FinalizeCounts(result);
}

void ApplyClassification(
    std::vector<GridScanCell>& cells,
    GridScanResult& result,
    const GridRecognitionResult& recognition,
    const std::vector<GridClassifyTemplate>& templates,
    const GridScanOptions& options,
    const GridClassifyOptions& classifyOptions,
    cv::Size imageSize,
    int viewportStartRow)
{
    const std::vector<std::size_t> occupiedIndices = CellIndices(cells);
    GridClassificationResult classification =
        ClassifyGridCells(recognition, templates, options.recognition, classifyOptions, imageSize, occupiedIndices);
    ApplyClassifications(cells, classification, result.cols, viewportStartRow, options.unknownTemplateId);
}

void CommitCurrentFrame(
    SessionState& session,
    GridScanResult& result,
    const GridRecognitionResult& recognition,
    const std::vector<GridClassifyTemplate>& templates,
    const GridScanOptions& options,
    const GridClassifyOptions& classifyOptions,
    const GridHashSnapshot& currentSnapshot,
    cv::Size imageSize,
    int resolvedRowOffset,
    bool resolverUsed)
{
    const int previousViewportStartRow = session.viewportStartRow;
    const int currentViewportStartRow = previousViewportStartRow + resolvedRowOffset;
    std::vector<GridScanCell> currentCells =
        PlaceGridCells(currentViewportStartRow, recognition, options, imageSize, options.unknownTemplateId);
    ApplyClassification(currentCells, result, recognition, templates, options, classifyOptions, imageSize, currentViewportStartRow);

    HideSessionCells(session.cells);
    UpsertSessionCells(session.cells, currentCells);
    session.snapshot = currentSnapshot;
    session.viewportStartRow = currentViewportStartRow;
    session.cols = result.cols;
    session.lastPositiveRowOffset = resolvedRowOffset;
    session.endConfirmations = 0;
    session.pending.reset();

    result.success = true;
    result.message = resolverUsed ? "Grid resolver committed current frame" : "Grid delta committed current frame";
    result.incrementalUsed = true;
    result.pendingResolved = resolverUsed;
    result.pendingStored = false;
    result.hasProgress = true;
    result.reachedEnd = false;
    result.resolverUsed = resolverUsed;
    result.resolverSuccess = resolverUsed;
    result.fallbackUsed = resolverUsed;
    result.previousViewportStartRow = previousViewportStartRow;
    result.currentViewportStartRow = currentViewportStartRow;
    result.resolvedRowOffset = resolvedRowOffset;
    result.endConfirmations = session.endConfirmations;
    result.dispatchableCells = currentCells;
    result.sessionCols = result.cols;
    result.cells = ToSortedCells(session.cells);
    FinalizeCounts(result);
}

bool IsStrongZeroOffset(const GridDeltaResult& delta, const GridScanResult& result, const GridScanOptions& options)
{
    return delta.rowOffset == 0 && delta.comparedCells >= result.cols * 2
           && delta.averageDistance <= static_cast<double>(options.matchDistanceThreshold)
           && delta.matchRatio >= std::clamp(options.endMinMatchRatio, 0.0, 1.0);
}

int VisibleSessionRowCount(const SessionState& session)
{
    std::vector<int> rows;
    for (const auto& entry : session.cells) {
        if (!entry.second.visible) {
            continue;
        }
        rows.push_back(entry.first.first);
    }
    std::sort(rows.begin(), rows.end());
    rows.erase(std::unique(rows.begin(), rows.end()), rows.end());
    return static_cast<int>(rows.size());
}

bool HasZeroOffsetNewVisibleRows(const SessionState& session, const GridRecognitionResult& recognition)
{
    return static_cast<int>(recognition.grid.rows.size()) > VisibleSessionRowCount(session);
}

void StorePending(SessionState& session, GridScanResult& result, const GridHashSnapshot& currentSnapshot, std::string reason)
{
    session.pending = SessionState::PendingState { currentSnapshot };
    result.success = true;
    result.message = "Grid delta unresolved; stored pending frame";
    result.incrementalUsed = true;
    result.pendingStored = true;
    result.pendingResolved = false;
    result.resolverUsed = false;
    result.resolverSuccess = false;
    result.fallbackUsed = false;
    result.hasProgress = false;
    result.reachedEnd = false;
    result.previousViewportStartRow = session.viewportStartRow;
    result.currentViewportStartRow = session.viewportStartRow;
    result.endConfirmations = session.endConfirmations;
    result.unresolvedReason = std::move(reason);
    result.sessionCols = session.cols;
    result.cells = ToSortedCells(session.cells);
    FinalizeCounts(result);
}

bool TryResolvePending(
    SessionState& session,
    GridScanResult& result,
    const GridRecognitionResult& recognition,
    const std::vector<GridClassifyTemplate>& templates,
    const GridScanOptions& options,
    const GridClassifyOptions& classifyOptions,
    const GridHashSnapshot& currentSnapshot,
    cv::Size imageSize)
{
    if (!session.pending) {
        return false;
    }

    const GridDeltaResult committedToPending =
        ComputeGridDelta(session.snapshot, session.pending->snapshot, { options.matchDistanceThreshold, options.minMatchRatio });
    const GridDeltaResult pendingToCurrent =
        ComputeGridDelta(session.pending->snapshot, currentSnapshot, { options.matchDistanceThreshold, options.minMatchRatio });
    if (!committedToPending.reliable || committedToPending.rowOffset <= 0 || !pendingToCurrent.reliable
        || pendingToCurrent.rowOffset <= 0) {
        result.resolverUsed = true;
        result.resolverSuccess = false;
        result.fallbackUsed = true;
        result.transitionRowOffset = pendingToCurrent.rowOffset;
        result.transitionReliable = pendingToCurrent.reliable;
        result.transitionHasProgress = pendingToCurrent.hasProgress;
        result.transitionAverageDistance = pendingToCurrent.averageDistance;
        result.transitionMatchRatio = pendingToCurrent.matchRatio;
        result.unresolvedReason = "pending resolver evidence was not reliable";
        return false;
    }

    const int resolvedRowOffset = committedToPending.rowOffset + pendingToCurrent.rowOffset;
    result.transitionRowOffset = pendingToCurrent.rowOffset;
    result.transitionReliable = pendingToCurrent.reliable;
    result.transitionHasProgress = pendingToCurrent.hasProgress;
    result.transitionAverageDistance = pendingToCurrent.averageDistance;
    result.transitionMatchRatio = pendingToCurrent.matchRatio;
    CommitCurrentFrame(
        session,
        result,
        recognition,
        templates,
        options,
        classifyOptions,
        currentSnapshot,
        imageSize,
        resolvedRowOffset,
        true);
    return true;
}

} // namespace

void HandleBeamTransition(
    SessionState& session,
    GridScanResult& result,
    const GridRecognitionResult& recognition,
    const std::vector<GridClassifyTemplate>& templates,
    const GridScanOptions& options,
    const GridClassifyOptions& classifyOptions,
    const GridHashSnapshot& currentSnapshot,
    const GridDeltaResult& delta,
    cv::Size imageSize)
{
    result.previousViewportStartRow = session.viewportStartRow;

    if (delta.reliable && delta.rowOffset > 0) {
        CommitCurrentFrame(
            session,
            result,
            recognition,
            templates,
            options,
            classifyOptions,
            currentSnapshot,
            imageSize,
            delta.rowOffset,
            false);
        return;
    }

    if (IsStrongZeroOffset(delta, result, options)) {
        if (HasZeroOffsetNewVisibleRows(session, recognition)) {
            CommitCurrentFrame(session, result, recognition, templates, options, classifyOptions, currentSnapshot, imageSize, 0, false);
            result.message = "Grid zero-offset committed expanded current frame";
            return;
        }
        session.pending.reset();
        ++session.endConfirmations;
        session.snapshot = currentSnapshot;
        const bool reachedEnd = session.endConfirmations >= kRequiredEndConfirmations;
        KeepSessionResult(result, session, reachedEnd, reachedEnd ? "Grid delta reached end" : "Grid delta zero-offset confirmation");
        result.incrementalUsed = true;
        result.pendingStored = false;
        result.pendingResolved = false;
        result.resolvedRowOffset = 0;
        result.endConfirmations = session.endConfirmations;
        return;
    }

    if (TryResolvePending(session, result, recognition, templates, options, classifyOptions, currentSnapshot, imageSize)) {
        return;
    }

    StorePending(session, result, currentSnapshot, result.unresolvedReason.empty() ? "delta was not reliable" : result.unresolvedReason);
}

} // namespace recogrid
