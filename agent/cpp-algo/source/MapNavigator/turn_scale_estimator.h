#pragma once

#include <algorithm>
#include <cmath>

#include "navi_config.h"

namespace mapnavigator
{

enum class TurnScaleBucketId
{
    Global,
};

struct TurnScaleBucket
{
    double units_per_degree = ComputeDefaultUnitsPerDegree();
    double alpha = kTurnLearningConfig.turn_scale_smoothing_alpha;
    double bootstrap_alpha = kTurnLearningConfig.turn_scale_smoothing_alpha;
    double min_units_per_degree = kTurnLearningConfig.turn_scale_min_units_per_degree;
    double max_units_per_degree = kTurnLearningConfig.turn_scale_max_units_per_degree;
    double min_observed_degrees = kTurnLearningConfig.min_observed_degrees;
    int accepted_samples = 0;
};

struct TurnScaleEstimator
{
    TurnScaleEstimator() { ConfigureGlobal(ComputeDefaultUnitsPerDegree()); }

    void ConfigureGlobal(
        double default_units_per_degree,
        double min_units_per_degree = kTurnLearningConfig.turn_scale_min_units_per_degree,
        double max_units_per_degree = kTurnLearningConfig.turn_scale_max_units_per_degree,
        double min_observed_degrees = kTurnLearningConfig.min_observed_degrees)
    {
        global = MakeBucket(
            default_units_per_degree,
            kTurnLearningConfig.turn_scale_smoothing_alpha,
            kTurnLearningConfig.turn_scale_smoothing_alpha,
            min_units_per_degree,
            max_units_per_degree,
            min_observed_degrees);
    }

    int DegreesToUnits(double degrees) const { return static_cast<int>(std::round(degrees * global.units_per_degree)); }

    double PredictDegreesFromUnits(int units) const
    {
        if (global.units_per_degree <= 0.0) {
            return 0.0;
        }
        return static_cast<double>(units) / global.units_per_degree;
    }

    double UnitsPerDegreeForDegrees([[maybe_unused]] double degrees) const { return global.units_per_degree; }

    double UnitsPerDegreeForUnits([[maybe_unused]] int units) const { return global.units_per_degree; }

    int AcceptedSamplesForUnits([[maybe_unused]] int units) const { return global.accepted_samples; }

    bool NeedsBootstrap() const { return global.accepted_samples < kTurnLearningConfig.bootstrap_target_samples; }

    bool NeedsBootstrapForUnits([[maybe_unused]] int units) const { return NeedsBootstrap(); }

    void PrimeAcceptedSamples(int min_samples) { global.accepted_samples = std::max(global.accepted_samples, min_samples); }

    bool UpdateFromSample(int units, double observed_degrees)
    {
        observed_degrees = std::abs(observed_degrees);
        if (std::abs(units) < kTurnLearningConfig.min_sample_units) {
            return false;
        }

        TurnScaleBucket& bucket = global;
        if (observed_degrees < bucket.min_observed_degrees || observed_degrees > kTurnLearningConfig.max_observed_degrees) {
            return false;
        }

        double sample = std::abs(static_cast<double>(units)) / observed_degrees;
        sample = std::clamp(sample, bucket.min_units_per_degree, bucket.max_units_per_degree);

        const double alpha = bucket.accepted_samples < kTurnLearningConfig.bootstrap_target_samples ? bucket.bootstrap_alpha : bucket.alpha;
        bucket.units_per_degree =
            std::clamp(bucket.units_per_degree * (1.0 - alpha) + sample * alpha, bucket.min_units_per_degree, bucket.max_units_per_degree);
        bucket.accepted_samples++;
        return true;
    }

    TurnScaleBucketId ResolveBucketForDegrees([[maybe_unused]] double degrees) const { return TurnScaleBucketId::Global; }

    TurnScaleBucketId ResolveBucketForUnits([[maybe_unused]] int units) const { return TurnScaleBucketId::Global; }

    static const char* BucketName(TurnScaleBucketId bucket_id)
    {
        switch (bucket_id) {
        case TurnScaleBucketId::Global:
            return "global";
        }
        return "unknown";
    }

    const char* BucketNameForUnits(int units) const { return BucketName(ResolveBucketForUnits(units)); }

    TurnScaleBucket global {};

private:
    static TurnScaleBucket MakeBucket(
        double default_units_per_degree,
        double alpha,
        double bootstrap_alpha,
        double min_units_per_degree,
        double max_units_per_degree,
        double min_observed_degrees)
    {
        TurnScaleBucket bucket;
        bucket.units_per_degree = std::clamp(default_units_per_degree, min_units_per_degree, max_units_per_degree);
        bucket.alpha = alpha;
        bucket.bootstrap_alpha = std::max(alpha, bootstrap_alpha);
        bucket.min_units_per_degree = min_units_per_degree;
        bucket.max_units_per_degree = max_units_per_degree;
        bucket.min_observed_degrees = min_observed_degrees;
        bucket.accepted_samples = 0;
        return bucket;
    }
};

} // namespace mapnavigator
