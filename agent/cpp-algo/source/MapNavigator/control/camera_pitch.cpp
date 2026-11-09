#include "camera_pitch.h"

#include <array>
#include <cstddef>

namespace mapnavigator
{

namespace
{

struct PitchSample
{
    double elevation_deg = 0.0;
    double fraction = 0.0;
};

constexpr std::array<PitchSample, 16> kPitchSamples { {
    { 84.3, 0.0 },
    { 80.0, 0.0095 },
    { 70.0, 0.0332 },
    { 60.0, 0.0604 },
    { 50.0, 0.0933 },
    { 40.0, 0.1349 },
    { 30.0, 0.1882 },
    { 20.0, 0.2549 },
    { 10.0, 0.3363 },
    { 0.0, 0.4339 },
    { -10.0, 0.5436 },
    { -20.0, 0.6587 },
    { -30.0, 0.7704 },
    { -40.0, 0.8701 },
    { -50.0, 0.9578 },
    { -55.7, 1.0 },
} };

} // namespace

double CameraVerticalFractionForElevation(double elevation_deg)
{
    if (elevation_deg >= kPitchSamples.front().elevation_deg) {
        return kPitchSamples.front().fraction;
    }
    for (size_t index = 1; index < kPitchSamples.size(); ++index) {
        const PitchSample& upper = kPitchSamples[index - 1];
        const PitchSample& lower = kPitchSamples[index];
        if (elevation_deg >= lower.elevation_deg) {
            const double t = (upper.elevation_deg - elevation_deg) / (upper.elevation_deg - lower.elevation_deg);
            return upper.fraction + (lower.fraction - upper.fraction) * t;
        }
    }
    return kPitchSamples.back().fraction;
}

} // namespace mapnavigator
