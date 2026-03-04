<!-- markdownlint-disable MD060 -->

# Development Guide - Custom Action Reference

`Custom` is a generic node type in the Pipeline used to invoke **custom actions**.  
The concrete logic is registered on the project side via `MaaResourceRegisterCustomAction` (for example, implementations under `agent/go-service`), while the Pipeline is only responsible for **parameter passing and scheduling**.

Unlike normal click/recognition nodes, `Custom` does not limit what the action actually does—  
as long as it is registered during the resource loading stage, it can be called in any Pipeline in a unified way, for example:

- Take a screenshot once and save it locally.
- Perform complex multi-step interactions (long-press, drag, combo keys, etc.).
- Do statistics, logging, or telemetry reporting.

---

<!-- markdownlint-enable MD060 -->
