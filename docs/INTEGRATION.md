# PipeGen integration handoff

The backend is ready to accept normalized frame observations. This is an **internal NavForge contract**, not a claim about PipeGen's export format. Inspect actual generated metadata before implementing an adapter. Do not modify the vendor prerequisite checks or replace the judged path with an unrelated detector.

1. Complete the real PipeGen Requirement → Pipeline IR → Model → Compiler → Run workflow. Save stage screenshots, generated source/configs and export metadata under `pipegen/export/` and `evidence/` as licenses allow.
2. Capture a small raw sample containing an object frame, a genuinely empty inferred frame, tracker metadata if available, video timestamps and frame numbers. Record model class labels, input dimensions and any coordinate scaling applied by DeepStream.
3. Implement a thin adapter from that verified format to the contract below. Normalize boxes into the **original configured video resolution**, rather than the detector's resized/letterboxed resolution. One request represents one completed inference frame, including all objects or an explicit empty array. Frames skipped by inference must not be emitted as clear frames.
4. Replace example camera polygons and lane associations using your fixed footage. Validate visible floor regions against known landmarks. Set a new `calibration_version`, correct `image_size`, and set `calibration_verified: true` only after checking alignment. Restart the backend with that config. Uncovered edges remain unknown.
5. Start the backend with a private ingestion token. Send frames to its loopback ingestion listener with `Authorization: Bearer <token>` and `Content-Type: application/json`.
6. Record the real frame → ROI transition → route version causal chain, plus the TensorRT compiler/speed report, actual video run and GPU utilization. Update evidence statuses only when the corresponding artifact exists.

## Frame contract v1.0

Illustrative shape only; this is not inference evidence:

```json
{
  "schema_version": "1.0",
  "event_id": "unique-camera-session-frame-id",
  "source": "pipegen-deepstream",
  "camera_id": "cam01",
  "session_id": "unique-run-id",
  "frame_index": 123,
  "video_ts_ms": 4920,
  "image_size": [1280, 720],
  "calibration_version": "your-verified-calibration-version",
  "objects": [
    {"track_id":"7", "class_label":"person", "confidence":0.91, "bbox_xywh_px":[318,226,70,174]}
  ]
}
```

`objects: []` means an inferred frame with no detections. The HLD's illustrative per-object message was refined to a frame envelope so clearing cannot be inferred from missing object messages. Track ID is optional and scoped to camera/session. The engine maps the bounding-box bottom center into configured image-space polygons; it does not infer world depth or accept client-selected ROI IDs.

All fields except `track_id` are required. IDs must be stable, frame index nonnegative, timestamps monotonic in each session, confidence within `[0,1]`, and boxes positive-sized and within the configured image. Request limit: 256 KiB, at most 256 objects. Unknown fields, malformed metadata and resolution/calibration mismatches are rejected. Each frame is globally deduplicated by event ID and camera/session/frame index. A new session retires the previous one permanently; use a new session ID on pipeline/video restart. Persist accepted frames and retry the same event ID after transport failures; HTTP 409 indicates duplicate/out-of-order input and must not be retried under a new event ID.

Durations use video timestamps, while a 2 s default receive timeout returns covered regions to unknown. Gaps over that timeout reset temporal evidence. The selected detector's actual inference rate must support the configured consecutive-frame policy; tune against footage. This first implementation treats persistent configured classes as a **demo exclusion rule**, not proof of physical impassability. Configure `risk_penalty`, class filters and time thresholds in the map file.

## Remaining integration work

- Verify PipeGen's vendor-supported WSL2/Docker path, output transport, model class list and GPU execution.
- Implement and test the real adapter with captured vendor fixtures; no assumed MQTT schema.
- Calibrate the graph/polygons for the selected MP4 and connect any container networking deliberately.
- Evaluate clear/transient/sustained obstruction segments with labels; collect measured latency/FPS/memory and occupancy precision/recall. No performance results are currently asserted.
