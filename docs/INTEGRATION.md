# PipeGen integration

## Verified export format

The supplied `pipeline/deepstream/parsers/frame_json.h` and `meta_json.cpp` produce a document with `pipeline: "video_json_export"`, `min_confidence`, and a `frames` array. Each completed frame has:

```json
{"frame":3,"source_id":0,"pts_ns":125000000,"width":1920,"height":1080,"objects":[{"id":0,"class_id":0,"label":"forklift","confidence":0.3898,"bbox":{"left":81.4,"top":0.0,"width":1831.5,"height":1074.7},"tracker_confidence":0.9608}]}
```

This is an actual captured frame record. It demonstrates compatibility, not correct localization. The exporter explicitly states that width, height and boxes are in **nvstreammux output coordinates**, not detector input or necessarily original-video coordinates. NavForge therefore requires exact agreement with calibration image size and performs no guessed letterbox conversion.

`internal/pipegen` streams this envelope, converts nanoseconds to milliseconds, preserves labels/confidence/track IDs, generates stable event IDs within a fresh session, and submits observations to the existing transactional ingestion service. It accepts explicit empty object arrays, rejects missing or malformed fields, nonmonotonic frames/timestamps, unexpected source IDs and resolution mismatches. Only up to 0.11 pixel of exporter rounding overflow is clipped. Invalid detections fail the input rather than being removed and interpreted as free space.

The exporter confidence filter must be at or below the occupancy threshold. Set `JSON_MIN_CONF=0.0`. Use `interval=0` in the detector config: the supplied format does not distinguish skipped inference from empty inference, so skipped/tracker-only frames must not be presented as clear evidence. The integrated deployment config uses interval zero.

## Replay saved inference

```powershell
./scripts/Start-NavForge.ps1
```

Default source: `navforge-gpu-deployment/results/metadata.json`. Default MP4: `Downloads/Camera1.mp4` when available, otherwise the adjacent exported `output.mp4`. Override with `-Video <path>`. Default map: `configs/warehouse.pipegen.json`. A new session is generated on every launch and stored with each observation. Input mode is explicitly `replay` in state and audit events. Video seeks to metadata timestamps; this is not a frame-accurate synchronized player.

Video preview defaults to independent playback. Enable `Follow metadata time` to seek alongside observations. The user confirmed Downloads/Camera1.mp4 as the original source. Its MP4 headers contain 1,254 samples at 24 fps over 52.25 seconds, matching the export. Browser decoding and a seek to 52.208 seconds pass. The earlier 21-second browser observation was not a valid duration/provenance conclusion and is superseded by these checks. Video responses now disable caching and use a per-session URL to prevent stale byte ranges when recordings change. HTTP byte-range serving and full-file proxy integrity were checked. Out-of-duration observations are flagged; a decoded-still fallback remains available for other media failures.

`-Speed 1` paces by video timestamps, `-Speed 2` requests twice playback speed, and `-Speed 0` imports as fast as storage permits. Actual throughput depends on processing/storage. At end-of-file the dashboard shows `complete`; after the receive timeout, occupancy becomes unknown. Failures appear in the panel and backend logs.

## Run the GPU export

The existing deployment requires the vendor runtime providing `nvinfer_backport` and `nvcustomprocess`, plus compatible CUDA/TensorRT/DeepStream libraries. Do not substitute a plain DeepStream image and claim that the PipeGen app has run. No existing PipeGen container was available during integration verification.

From your normal WSL terminal, after restoring/starting the vendor-supported container:

```bash
cd '/mnt/e/Research & Projects/NavForge/navforge-gpu-deployment'
./deploy-wsl.sh '/mnt/c/Users/Atharva/Downloads/Camera1.mp4'
```

The launcher now defaults to the supplied **forklift.fp16.plan** and derives the class count from its `forklift`, `person` label file. It no longer overwrites this configuration with the unrelated COCO-80 YOLOv8n engine. Selecting another model requires a matching engine, label file, parser/output layout and class count. Existing original export files remain separate.

The source compile report under `model_stage/` belongs to **yolov8n**, not the active forklift engine. Do not attribute its pseudo-ground-truth accuracy metrics or build report to the forklift model. Build duration is not inference latency/FPS.

## Follow inference inside the container

After deploying the package, use a fresh output path for each run. In Windows PowerShell, start the application first:

```powershell
./scripts/Start-NavForge.ps1 -Mode Live -Container craftifai-amd64-v3 -Metadata /opt/navforge-gpu/output/run001/metadata.json -Video 'C:/Users/Atharva/Downloads/Camera1.mp4'
```

Then in WSL:

```bash
docker exec craftifai-amd64-v3 mkdir -p /opt/navforge-gpu/output/run001
docker exec \
  -e VIDEO_URI=file:///workspace/navforge_input.mp4 \
  -e JSON_OUT=/opt/navforge-gpu/output/run001/metadata.json \
  -e VIDEO_OUT=/opt/navforge-gpu/output/run001/output.mp4 \
  craftifai-amd64-v3 bash /opt/navforge-gpu/run-in-container.sh
```

NavForge reads `docker exec <container> tail -c +1 -F <path>` through a pipe. No container-to-Windows HTTP forwarding or exposed ingestion token is needed. The source file is flushed each frame by the existing vendor writer. Following stops at the JSON document's closing brace. Use the same Docker context/daemon as the running container. Never reuse a completed output file and call it a new live run.

Alternatively, if the container already has a host-mounted output directory, omit `-Container` and pass its Windows file path to `-Metadata` in Live mode. The file must not exist when the launcher starts. `NAVFORGE_SHARED_OUTPUT` in `deploy-wsl.sh` selects a container-visible mounted output path. File truncation requires a new input session/restart.

## Calibration and localization

The recording's metadata is 1920×1080. The integration config scales the original example polygon to that coordinate space but leaves it **unverified**. Its obstacle classes are `forklift` and `person`; confidence threshold remains 0.65. It does not infer that footage corresponds to the example lane graph.

Use the dashboard calibration editor to draw the visible floor polygon and associate it with a graph edge. Inspect the red box-bottom-center markers and all configured ROI associations. Download a draft or explicitly verified map, then restart with `-Config <downloaded-file>`. The bridge picks up the new calibration version at startup. Uncovered lanes remain unknown.

The supplied metadata has 1,254 frames, 1,251 frames containing objects, and 2,319 forklift detections. 1,898 boxes exceed 80% of the frame area. This warrants inspection of preprocessing, tensor layout/decoder, and footage before enabling occupancy changes. The adapter does not relabel, shrink, or fabricate detections to produce a convincing reroute.

## External HTTP contract

External producers can still send normalized frames to the loopback-only ingestion listener with a private `NAVFORGE_INGEST_TOKEN` of at least 24 characters:

```json
{
  "schema_version":"1.0",
  "input_mode":"live",
  "event_id":"unique-session-frame-id",
  "source":"pipegen-deepstream",
  "camera_id":"cam01",
  "session_id":"unique-run-id",
  "frame_index":3,
  "video_ts_ms":125,
  "image_size":[1920,1080],
  "calibration_version":"your-verified-version",
  "objects":[{"track_id":"0","class_label":"forklift","confidence":0.3898,"bbox_xywh_px":[81.4,0,1831.5,1074.7]}]
}
```

POST to `http://127.0.0.1:8081/internal/perception/events` with `Authorization: Bearer <token>` and `Content-Type: application/json`. Only `track_id` and `input_mode` are optional. Input mode may be `live` or `replay`. No client-selected ROI IDs are accepted. Limit: 256 KiB and 256 objects per frame. Unknown fields, calibration mismatches, invalid boxes and null/missing required fields are rejected.

Retry the same event ID after a transport failure. HTTP 409 denotes duplicate/out-of-order/retired-session input; do not retry with a fabricated new ID. Use a new session ID on genuine pipeline restart. Historical duplicate/session protection survives backend restart.

## Remaining evidence

Restore the vendor GPU runtime, verify detections against footage, complete calibration, and record actual frame → occupancy → route causation. Collect the active model's speed report and measured occupancy/latency metrics. Captured-fixture and synthetic calibration tests validate integration mechanics only.
