# Supplied PipeGen export locations

The user's export is preserved at the repository root:

- `PipelineIR.json`, `.pir_bindings.json`
- `model_stage/`, `model_compile_output/`
- `pipeline/deepstream/`

The supplied repackaged GPU deployment, active forklift model, metadata and MP4
are in `navforge-gpu-deployment/`. Integration uses its actual frame JSON format.
See `docs/INTEGRATION.md` and `evidence/manifest.json` for provenance and limitations.

The root YOLOv8n compilation report and the deployment's forklift engine are
different artifacts. Do not attribute one model's measurements to the other.
Large model/video binaries should be distributed as hashed artifacts rather than
blindly added to Git. Preserve vendor metadata and licensing.
