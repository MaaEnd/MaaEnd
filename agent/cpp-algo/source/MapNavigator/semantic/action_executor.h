#pragma once

namespace mapnavigator
{

struct Motion;
struct InputBackend;

void ExecuteSprint(Motion* motion_controller);
void ExecuteJump(InputBackend* input_backend);
void ExecuteInteract(InputBackend* input_backend);
void ExecuteFight(InputBackend* input_backend);

} // namespace mapnavigator
