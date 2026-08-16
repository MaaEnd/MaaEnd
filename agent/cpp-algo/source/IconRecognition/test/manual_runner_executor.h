#pragma once

#include <cstddef>
#include <exception>
#include <functional>
#include <vector>

namespace iconrecognition::test
{

using ManualRunnerCaseTask = std::function<void(std::size_t)>;

std::vector<std::exception_ptr> ExecuteManualRunnerCases(std::size_t case_count, std::size_t jobs, const ManualRunnerCaseTask& task);

} // namespace iconrecognition::test
