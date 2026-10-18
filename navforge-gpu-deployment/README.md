# NavForge GPU perception deployment (repackaged from user's PipeGen artifacts)

**Integration update:** NavForge now reads this package's real JSON export and can replay the supplied results or follow a growing file inside the container. See [../docs/INTEGRATION.md](../docs/INTEGRATION.md). The launcher defaults to the supplied `forklift.fp16.plan` with the matching two-label file, rather than overwriting it with COCO-80 settings. The original export under the repository root remains separate.

This package **contains the actual TensorRT FP16 engine**, generated DeepStream GStreamer code, detector parser, tracker configuration, metadata writer and .so files. It does **not bundle DeepStream, CUDA, TensorRT libraries, or the NVIDIA driver**. Run it inside the **existing** `craftifai-amd64-v3` PipeGen container which already provides those components (including `nvinfer_backport` and `nvcustomprocess`). It has not been GPU-executed in the author's environment; test on your WSL2 GPU.

## Run on your Windows + WSL2 machine

From your **normal WSL2 shell** (not the PipeGen internal terminal), extract the ZIP under your NavForge project and run:

```bash
cd '/mnt/e/Research & Projects/NavForge/navforge-gpu-deployment'
chmod +x deploy-wsl.sh
./deploy-wsl.sh '/mnt/c/Users/Atharva/Downloads/Camera1.mp4'
python3 diagnose-metadata.py results/metadata.json
```

The launcher copies the package into `/opt/navforge-gpu` within your existing PipeGen container, copies the input to `/workspace/navforge_input.mp4`, and runs the GPU pipeline. Results are copied to `results/`. The container must be running and the GPU/DeepStream image must be functional. Override `PIPEGEN_CONTAINER` if its name changes. Do not assume an exported TensorRT engine works on a different GPU or driver/TensorRT stack; rebuild if necessary.

## Fixed relative packaging paths

The original hardcoded project path is removed from the **active launcher and detector configuration**. `run-in-container.sh` resolves the package root at runtime and creates a DeepStream detector config with absolute paths appropriate to the **current container**. The original `../PipelineIR.json` retains the original workspace paths and is reference evidence, not a portable runtime config. Its duplicate deployment copy was removed.

## Known unresolved issues, based on the supplied artifacts

* The original log preserved at `../evidence/pipegen-yolov8n-original-run.txt` reports **1,254 processed frames and 0 detected objects** for YOLOv8n. The newer supplied `results/metadata.json` contains **2,319 forklift detections**, but 1,898 boxes exceed 80% of the frame area. Detection correctness is **not verified**; inspect preprocessing and parser tensor layout against the video.
* The current supplied `results/output.mp4` is 52,517,662 bytes and supports HTTP range playback through NavForge. This does not validate a fresh GPU encoding run in the present environment.
* The original `run.sh` refers to `nvinfer_backport` and `nvcustomprocess`, not ordinary `nvinfer`; they must exist in the target container. The supplied `.so` libraries rely on DeepStream ABI-compatible system libraries.
* The package writes metadata files; NavForge's `internal/pipegen` adapter streams those files into the validated Go ingestion service. External normalized HTTP producers remain supported.
* `run.sh` invokes make on launch; if the container lacks build headers, disable rebuild only after verifying shipped binaries work and are ABI compatible.

## Verification

Check `gst-inspect-1.0 nvinfer_backport`, `gst-inspect-1.0 nvcustomprocess`, and `nvidia-smi` in the container. Examine runtime errors, `results/metadata.json`, `results/output.mp4`, and inspect the first non-empty detection. A successful launcher exit alone does not prove object detection.
