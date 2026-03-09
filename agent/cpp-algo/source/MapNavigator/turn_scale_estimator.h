#pragma once

#include <algorithm>

#include "navi_config.h"

namespace mapnavigator
{

struct TurnScaleEstimator
{
    double units_per_degree = kDefaultPixelsPerDegree;
    double alpha = kTurnScaleSmoothingAlpha;
    double min_units_per_degree = kTurnScaleMinUnitsPerDegree;
    double max_units_per_degree = kTurnScaleMaxUnitsPerDegree;
    int accepted_samples = 0;

    int DegreesToUnits(double degrees) const { return static_cast<int>(std::round(degrees * units_per_degree)); }

    bool NeedsBootstrap() const { return accepted_samples < kTurnBootstrapTargetSamples; }

    bool IsWarmedUp() const { return !NeedsBootstrap(); }

    double PredictDegreesFromUnits(int units) const
    {
        if (units_per_degree <= 0.0) {
            return 0.0;
        }
        return static_cast<double>(units) / units_per_degree;
    }

    bool UpdateFromSample(int units, double observed_degrees)
    {
        observed_degrees = std::abs(observed_degrees);
        if (std::abs(units) < kTurnLearningMinSampleUnits) {
            return false;
        }
        if (observed_degrees < kTurnLearningMinObservedDegrees || observed_degrees > kTurnLearningMaxObservedDegrees) {
            return false;
        }

        double sample = std::abs(static_cast<double>(units)) / observed_degrees;
        sample = std::clamp(sample, min_units_per_degree, max_units_per_degree);

        units_per_degree = std::clamp(units_per_degree * (1.0 - alpha) + sample * alpha, min_units_per_degree, max_units_per_degree);
        accepted_samples++;
        return true;
    }
};

} // namespace mapnavigator
