#include "RecoGridTransition.h"

#include "RecoGridPlacement.h"

#include <algorithm>
#include <string>
#include <utility>

namespace recogrid
{
namespace
{

constexpr int kRequiredEndConfirmations = 2;
constexpr int kMaxConsecutiveFallbacks = 1;

enum class CommitSource
{
    Alignment,
    HistoricalFallback,
};

struct RejectionDetails
{
    std::string status;
    std::string reason;
};

void FillSessionResult(GridScanResult& result, const SessionState& session)
{
    result.incrementalUsed = true;
    result.sessionCols = session.cols;
    result.previousViewportStartRow = session.viewportStartRow;
    result.currentViewportStartRow = session.viewportStartRow;
    result.endConfirmations = session.endConfirmations;
    result.fallbackStreak = session.fallbackStreak;
    result.cells = ToSortedCells(session.cells);
    FinalizeCounts(result);
}

void KeepSessionResult(
    GridScanResult& result,
    const SessionState& session,
    bool reachedEnd,
    std::string message,
    std::string alignmentStatus)
{
    result.success = true;
    result.message = std::move(message);
    result.alignmentStatus = std::move(alignmentStatus);
    result.reachedEnd = reachedEnd;
    result.rowOffset = 0;
    result.hasProgress = false;
    result.fallbackUsed = false;
    result.newCellIndices.clear();
    result.dispatchableCells.clear();
    FillSessionResult(result, session);
}

void RejectCurrentFrame(GridScanResult& result, const SessionState& session, std::string alignmentStatus, std::string reason)
{
    result.success = false;
    result.message = "Grid transition requires another screenshot";
    result.alignmentStatus = std::move(alignmentStatus);
    result.unresolvedReason = std::move(reason);
    result.reachedEnd = false;
    result.rowOffset = 0;
    result.hasProgress = false;
    result.fallbackUsed = false;
    result.newCellIndices.clear();
    result.dispatchableCells.clear();
    FillSessionResult(result, session);
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
    int committedRowOffset,
    CommitSource source)
{
    const int previousViewportStartRow = session.viewportStartRow;
    const int currentViewportStartRow = previousViewportStartRow + committedRowOffset;
    std::vector<GridScanCell> currentCells =
        PlaceGridCells(currentViewportStartRow, recognition, options, imageSize, options.unknownTemplateId);
    ApplyClassification(currentCells, result, recognition, templates, options, classifyOptions, imageSize, currentViewportStartRow);

    HideSessionCells(session.cells);
    UpsertSessionCells(session.cells, currentCells);
    session.snapshot = currentSnapshot;
    session.viewportStartRow = currentViewportStartRow;
    session.cols = result.cols;
    session.endConfirmations = 0;
    session.recheck.reset();

    if (source == CommitSource::Alignment && committedRowOffset > 0) {
        session.lastPositiveRowOffset = committedRowOffset;
        session.fallbackStreak = 0;
    }
    else if (source == CommitSource::HistoricalFallback) {
        ++session.fallbackStreak;
    }

    result.success = true;
    result.incrementalUsed = true;
    result.hasProgress = true;
    result.reachedEnd = false;
    result.rowOffset = committedRowOffset;
    result.fallbackUsed = source == CommitSource::HistoricalFallback;
    result.previousViewportStartRow = previousViewportStartRow;
    result.currentViewportStartRow = currentViewportStartRow;
    result.endConfirmations = session.endConfirmations;
    result.fallbackStreak = session.fallbackStreak;
    result.dispatchableCells = currentCells;
    result.sessionCols = result.cols;
    result.cells = ToSortedCells(session.cells);

    if (committedRowOffset > 0) {
        result.newCellIndices = NewCellIndicesForOffset(currentSnapshot, committedRowOffset);
    }
    else {
        result.newCellIndices.clear();
    }

    switch (source) {
    case CommitSource::Alignment:
        result.message = "Grid delta committed current frame";
        result.alignmentStatus = "accepted";
        result.unresolvedReason.clear();
        break;
    case CommitSource::HistoricalFallback:
        result.message = "Grid recheck committed last positive row offset";
        result.alignmentStatus = "fallback_accepted";
        break;
    }

    FinalizeCounts(result);
}

bool IsStrongZeroOffset(const GridDeltaResult& delta, int cols, const GridScanOptions& options)
{
    return delta.reliable && delta.rowOffset == 0 && delta.hasSufficientOverlap && delta.supportRows >= 2 && delta.comparedCells >= cols * 2
           && delta.averageDistance <= static_cast<double>(options.matchDistanceThreshold)
           && delta.matchRatio >= std::clamp(options.endMinMatchRatio, 0.0, 1.0);
}

bool IsStableRecheck(const SessionState::RecheckState& recheck, const GridHashSnapshot& currentSnapshot, const GridScanOptions& options)
{
    if (recheck.snapshot.rows != currentSnapshot.rows || recheck.snapshot.cols != currentSnapshot.cols) {
        return false;
    }

    const GridDeltaResult recheckDelta = ComputeGridDelta(
        recheck.snapshot,
        currentSnapshot,
        {
            options.matchDistanceThreshold,
            options.endMinMatchRatio,
            2,
        });
    return IsStrongZeroOffset(recheckDelta, currentSnapshot.cols, options);
}

RejectionDetails DescribeRejection(const GridDeltaResult& delta, const GridScanOptions& options)
{
    if (!delta.hasSufficientOverlap) {
        return { "insufficient_support", "alignment had fewer than two supported rows" };
    }
    if (delta.directionAmbiguous) {
        return { "direction_ambiguous", "forward and reverse alignment evidence was tied" };
    }
    if (delta.rowOffset < 0) {
        return { "direction_violation", "best supported alignment moved backward" };
    }
    if (!delta.reliable) {
        return { "low_confidence", "alignment evidence was below the required match ratio" };
    }
    if (delta.rowOffset == 0 && delta.matchRatio < std::clamp(options.endMinMatchRatio, 0.0, 1.0)) {
        return { "zero_unconfirmed", "zero-offset alignment was not strong enough for end confirmation" };
    }
    return { "no_progress", "alignment did not provide accepted forward progress" };
}

} // namespace

void HandleGridTransition(
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
            CommitSource::Alignment);
        return;
    }

    if (IsStrongZeroOffset(delta, result.cols, options)) {
        session.recheck.reset();
        ++session.endConfirmations;
        session.snapshot = currentSnapshot;
        const bool reachedEnd = session.endConfirmations >= kRequiredEndConfirmations;
        KeepSessionResult(
            result,
            session,
            reachedEnd,
            reachedEnd ? "Grid delta reached end" : "Grid delta zero-offset confirmation",
            reachedEnd ? "reached_end" : "end_confirmation");
        return;
    }

    RejectionDetails rejection = DescribeRejection(delta, options);
    const bool stableRecheck = session.recheck && IsStableRecheck(*session.recheck, currentSnapshot, options);
    if (stableRecheck && session.lastPositiveRowOffset > 0 && session.fallbackStreak < kMaxConsecutiveFallbacks) {
        result.unresolvedReason = rejection.reason;
        CommitCurrentFrame(
            session,
            result,
            recognition,
            templates,
            options,
            classifyOptions,
            currentSnapshot,
            imageSize,
            session.lastPositiveRowOffset,
            CommitSource::HistoricalFallback);
        return;
    }

    if (!stableRecheck) {
        session.recheck = SessionState::RecheckState { currentSnapshot };
        rejection.reason += "; stored current frame for same-viewport verification";
    }
    else if (session.lastPositiveRowOffset <= 0) {
        rejection.status = "fallback_unavailable";
        rejection.reason += "; no trusted positive row offset is available";
    }
    else {
        rejection.status = "fallback_limit_reached";
        rejection.reason += "; consecutive fallback limit reached";
    }

    RejectCurrentFrame(result, session, std::move(rejection.status), std::move(rejection.reason));
}

} // namespace recogrid
