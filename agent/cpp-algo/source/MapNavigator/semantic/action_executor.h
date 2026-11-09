#pragma once

namespace mapnavigator
{

struct Motion;
struct InputBackend;

void ExecuteSprint(Motion* motion_controller);
void ExecuteJump(InputBackend* input_backend, Motion* motion_controller);
void ExecuteInteract(InputBackend* input_backend, Motion* motion_controller);
void ExecuteFight(InputBackend* input_backend, Motion* motion_controller);

} // namespace mapnavigator
