#include "RecoGridTransition.h"

#include <algorithm>

namespace recogrid
{
namespace
{

void KeepSessionResult(GridScanResult& result, const SessionState& session, bool reachedEnd, std::string message)
{
    result.success = true;
    result.message = std::move(message);
    result.reachedEnd = reachedEnd;
    result.sessionCols = session.cols;
    result.cells = ToSortedCells(session.cells);
    FinalizeCounts(result);
}

std::vector<SessionState::PendingState> MakePendingStates(
    std::vector<PlacementCandidate> candidates,
    GridScanResult& result,
    const GridRecognitionResult& recognition,
    const std::vector<GridClassifyTemplate>& templates,
    const GridScanOptions& options,
    const GridClassifyOptions& classifyOptions,
    const GridHashSnapshot& currentSnapshot,
    cv::Size imageSize)
{
    std::vector<SessionState::PendingState> pendingStates;
    if (candidates.empty()) {
        return pendingStates;
    }

    const std::vector<std::size_t> occupiedIndices = CellIndices(candidates.front().cells);
    GridClassificationResult classification = ClassifyGridCells(
        recognition,
        templates,
        options.recognition,
        classifyOptions,
        imageSize,
        occupiedIndices);

    pendingStates.reserve(candidates.size());
    for (PlacementCandidate& candidate : candidates) {
        ApplyClassifications(
            candidate.cells,
            classification,
            result.cols,
            candidate.viewportStartRow,
            options.unknownTemplateId);

        SessionState::PendingState pending;
        pending.snapshot = currentSnapshot;
        pending.viewportStartRow = candidate.viewportStartRow;
        pending.cells = std::move(candidate.cells);
        pending.score = candidate.score;
        pendingStates.push_back(std::move(pending));
    }
    return pendingStates;
}

std::vector<PlacementCandidate> BuildCurrentPendingForBeam(
    const SessionState::BeamState& beam,
    const GridDeltaResult& sourceDelta,
    GridScanResult& result,
    const GridRecognitionResult& recognition,
    const GridScanOptions& options,
    const GridHashSnapshot& currentSnapshot,
    cv::Size imageSize,
    int placementBeamWidth)
{
    const int expectedOffset =
        sourceDelta.rowOffset > 0 ? sourceDelta.rowOffset :
                                    (beam.lastPositiveRowOffset > 0 ? beam.lastPositiveRowOffset : 1);
    GridDeltaResult candidateDelta = sourceDelta;
    candidateDelta.rowOffset = expectedOffset;
    const std::vector<int> candidateOffsets =
        BuildCandidateOffsets(candidateDelta, result.rows, beam.lastPositiveRowOffset);
    return BuildPlacementCandidates(
        beam.cells,
        beam.snapshot,
        currentSnapshot,
        recognition,
        options,
        imageSize,
        beam.viewportStartRow,
        beam.viewportStartRow + expectedOffset,
        candidateOffsets,
        placementBeamWidth);
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
    cv::Size imageSize,
    int placementBeamWidth)
{
    SessionState working = session;
    if (working.beams.empty()) {
        SessionState::BeamState beam;
        beam.snapshot = working.snapshot;
        beam.viewportStartRow = working.viewportStartRow;
        beam.cols = working.cols;
        beam.lastPositiveRowOffset = working.lastPositiveRowOffset;
        beam.cells = working.cells;
        beam.pending = working.pending;
        working.beams.push_back(std::move(beam));
    }

    std::vector<SessionState::BeamState> nextBeams;
    bool anyReachedEnd = false;
    for (const SessionState::BeamState& beam : working.beams) {
        if (beam.pending.empty()) {
            std::vector<PlacementCandidate> candidates = BuildCurrentPendingForBeam(
                beam,
                delta,
                result,
                recognition,
                options,
                currentSnapshot,
                imageSize,
                placementBeamWidth);
            std::vector<SessionState::PendingState> pendingStates = MakePendingStates(
                std::move(candidates),
                result,
                recognition,
                templates,
                options,
                classifyOptions,
                currentSnapshot,
                imageSize);
            if (pendingStates.empty()) {
                continue;
            }
            SessionState::BeamState nextBeam = beam;
            nextBeam.pending = std::move(pendingStates);
            nextBeams.push_back(std::move(nextBeam));
            continue;
        }

        for (const SessionState::PendingState& pending : beam.pending) {
            GridDeltaResult pendingDelta = ComputeGridDelta(
                pending.snapshot,
                currentSnapshot,
                { options.matchDistanceThreshold, options.minMatchRatio });

            const double weakMinMatchRatio =
                std::clamp(options.weakMinMatchRatio, 0.0, std::clamp(options.minMatchRatio, 0.0, 1.0));
            const bool weakProgress =
                pendingDelta.rowOffset > 0 && pendingDelta.comparedCells > 0 &&
                pendingDelta.matchRatio >= weakMinMatchRatio;
            const bool reachedEnd =
                pendingDelta.rowOffset == 0 &&
                pendingDelta.matchRatio >= std::clamp(options.endMinMatchRatio, 0.0, 1.0);
            if (!pendingDelta.reliable && !weakProgress && !reachedEnd) {
                continue;
            }

            SessionState::BeamState committed = beam;
            HideSessionCells(committed.cells);
            UpsertSessionCells(committed.cells, pending.cells);
            committed.snapshot = pending.snapshot;
            committed.viewportStartRow = pending.viewportStartRow;
            committed.cols = result.cols;
            const int confirmedRowOffset = pending.viewportStartRow - beam.viewportStartRow;
            if (confirmedRowOffset > 0) {
                committed.lastPositiveRowOffset = confirmedRowOffset;
            }
            committed.pending.clear();
            committed.score = beam.score + pending.score + (pendingDelta.reliable ? 100.0 : 0.0);

            if (reachedEnd) {
                anyReachedEnd = true;
                nextBeams.push_back(std::move(committed));
                continue;
            }

            const std::vector<int> candidateOffsets =
                BuildCandidateOffsets(pendingDelta, result.rows, committed.lastPositiveRowOffset);
            const int expectedOffset =
                pendingDelta.rowOffset > 0 ? pendingDelta.rowOffset :
                                             (committed.lastPositiveRowOffset > 0 ? committed.lastPositiveRowOffset : 1);
            std::vector<PlacementCandidate> currentCandidates = BuildPlacementCandidates(
                committed.cells,
                pending.snapshot,
                currentSnapshot,
                recognition,
                options,
                imageSize,
                pending.viewportStartRow,
                pending.viewportStartRow + expectedOffset,
                candidateOffsets,
                placementBeamWidth);
            std::vector<SessionState::PendingState> currentPending = MakePendingStates(
                std::move(currentCandidates),
                result,
                recognition,
                templates,
                options,
                classifyOptions,
                currentSnapshot,
                imageSize);
            for (SessionState::PendingState& current : currentPending) {
                SessionState::BeamState nextBeam = committed;
                nextBeam.score += current.score;
                nextBeam.pending.push_back(std::move(current));
                nextBeams.push_back(std::move(nextBeam));
            }
        }
    }

    if (nextBeams.empty()) {
        KeepSessionResult(result, session, false, "Grid beam had no resolvable path; kept previous scan session");
        return;
    }

    std::sort(nextBeams.begin(), nextBeams.end(), [](const SessionState::BeamState& lhs, const SessionState::BeamState& rhs) {
        if (lhs.score != rhs.score) {
            return lhs.score > rhs.score;
        }
        return lhs.cells.size() > rhs.cells.size();
    });
    if (static_cast<int>(nextBeams.size()) > placementBeamWidth) {
        nextBeams.resize(static_cast<std::size_t>(placementBeamWidth));
    }

    const SessionState::BeamState& best = nextBeams.front();
    SessionState nextSession;
    nextSession.snapshot = best.snapshot;
    nextSession.viewportStartRow = best.viewportStartRow;
    nextSession.cols = best.cols;
    nextSession.lastPositiveRowOffset = best.lastPositiveRowOffset;
    nextSession.cells = best.cells;
    nextSession.pending = best.pending;
    nextSession.beams = std::move(nextBeams);

    result.success = true;
    result.message = anyReachedEnd ? "Grid beam reached end" : "Grid beam advanced";
    result.incrementalUsed = true;
    result.pendingStored = !best.pending.empty();
    result.pendingResolved = true;
    result.reachedEnd = anyReachedEnd && best.pending.empty();
    result.sessionCols = result.cols;
    result.cells = ToSortedCells(best.cells);
    FinalizeCounts(result);
    session = std::move(nextSession);
}

} // namespace recogrid
