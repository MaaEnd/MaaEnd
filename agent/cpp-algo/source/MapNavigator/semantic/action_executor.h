#pragma once

namespace mapnavigator
{

struct Motion;
struct InputBackend;

namespace walkmode
{

struct WalkBelief;

} // namespace walkmode

void ExecuteSprint(Motion* motion_controller, walkmode::WalkBelief* walk);
void ExecuteJump(InputBackend* input_backend);
void ExecuteInteract(InputBackend* input_backend);
void ExecuteFight(InputBackend* input_backend);

} // namespace mapnavigator
