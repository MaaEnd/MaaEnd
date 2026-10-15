#pragma once

#include <meojson/json.hpp>

namespace iconrecognition::detail
{

json::object ApplyAttach(const json::object& parameters, const json::object& node_data);

} // namespace iconrecognition::detail
