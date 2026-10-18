# Local artifacts and repository contents

The repository keeps application source, tests, map/policy configuration, deployment scripts, PipeGen source/IR and compilation reports. `evidence/manifest.json` records artifact provenance and available hashes. The supplied `navforge-gpu-deployment/results/metadata.json` remains versionable for reproducible replay.

Large binary artifacts are retained locally but ignored by Git:

| Local path | Purpose / how to restore |
| --- | --- |
| `navforge-gpu-deployment/model_stage/forklift.fp16.plan` | Active TensorRT engine; restore the matching vendor export or rebuild in a compatible runtime |
| `navforge-gpu-deployment/model_stage/forklift.onnx` | Active model source; restore from the supplied model artifacts |
| `model_stage/yolov8n.fp16.plan` | Original separate YOLOv8n compilation artifact; its reports remain in `model_stage/` |
| `pipeline/deepstream/build/*.so` | Original generated shared libraries; rebuild using the adjacent parsers/Makefile in the compatible vendor runtime |
| `navforge-gpu-deployment/pipeline/deepstream/build/*.so` | Active deployment libraries; rebuild in the compatible vendor runtime |
| `navforge-gpu-deployment/results/output.mp4` | Saved annotated output; regenerate by running the GPU pipeline |
| `Downloads/Camera1.mp4` | User-confirmed original recording, outside the repository; pass another location with `-Video` |

For replay, restore the matching source video or exported MP4 and use the retained metadata. The launcher prefers Downloads/Camera1.mp4 and otherwise uses the exported MP4. A fresh GPU run additionally needs the active engine, labels/parser/configuration and vendor runtime. The deployment script copies only pipeline inputs, model files and its launcher into the container.

`data/` contains the local audit database, dependency/build caches and runtime logs; `bin/` and the Next.js build/dependency directories are generated and ignored. The audit database and installed dependencies were preserved during cleanup.

Cleanup removed the unused `src1/` detector experiment, its incomplete model downloads, the duplicate deployment archive, duplicate deployment copies of the original YOLOv8n engine and IR, empty shared-library backups, compiler timing cache and temporary browser/test-session files. Original compile reports and one copy of the original run log remain as evidence. No required active model, replay recording or production audit history was deleted.
