// Custom detection (bbox) parser (generated).
// Scaffold validates the output layer and exposes the raw tensor(s); the DECODE
// block (per-model) populates `objectList` with NvDsInferObjectDetectionInfo.
#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

#include "nvdsinfer_custom_impl.h"

extern "C" bool ParseConfigDetPgie(
    std::vector<NvDsInferLayerInfo> const& outputLayersInfo,
    NvDsInferNetworkInfo const& networkInfo,
    NvDsInferParseDetectionParams const& detectionParams,
    std::vector<NvDsInferObjectDetectionInfo>& objectList);

CHECK_CUSTOM_PARSE_FUNC_PROTOTYPE(ParseConfigDetPgie);

extern "C" bool ParseConfigDetPgie(
    std::vector<NvDsInferLayerInfo> const& outputLayersInfo,
    NvDsInferNetworkInfo const& networkInfo,
    NvDsInferParseDetectionParams const& detectionParams,
    std::vector<NvDsInferObjectDetectionInfo>& objectList)
{
    if (outputLayersInfo.empty()) return false;
    auto const& layer0 = outputLayersInfo[0];

    const unsigned numClasses = detectionParams.numClassesConfigured;
    const float netW = static_cast<float>(networkInfo.width);
    const float netH = static_cast<float>(networkInfo.height);
    const float* data = static_cast<const float*>(layer0.buffer);

    static bool logged = false;
    if (!logged) {
        logged = true;
        fprintf(stderr, "[YOLOv8n-det] first call: layer=%s numDims=%d "
                "networkInput=%.0fx%.0f numClasses=%u outputs=%zu\n",
                layer0.layerName ? layer0.layerName : "?",
                layer0.inferDims.numDims, netW, netH, numClasses,
                outputLayersInfo.size());
        for (int dd = 0; dd < layer0.inferDims.numDims; ++dd)
            fprintf(stderr, "  d[%d] = %u\n", dd, layer0.inferDims.d[dd]);
    }

    // ==== BEGIN GENERATED DECODE ====
    // YOLOv8 output (batch stripped): [84, 8400] — numDims=2, d[0]=84, d[1]=8400
    // OR transposed export: [8400, 84].
    // Detect layout by checking which dim equals 4+numClasses.

    if (layer0.inferDims.numDims < 2) return false;

    const unsigned dimA = layer0.inferDims.d[0];
    const unsigned dimB = layer0.inferDims.d[1];
    const unsigned featureLen = 4u + numClasses; // 84 for COCO-80

    bool transposed = false; // true when layout is [N, featureLen] i.e. already row-major
    unsigned numDetections = 0;

    if (dimA == featureLen && dimB != featureLen) {
        // [featureLen, N] — standard YOLOv8 output
        transposed = false;
        numDetections = dimB;
    } else if (dimB == featureLen && dimA != featureLen) {
        // [N, featureLen] — transposed export
        transposed = true;
        numDetections = dimA;
    } else if (dimA == featureLen && dimB == featureLen) {
        // ambiguous square — assume standard [featureLen, N]
        transposed = false;
        numDetections = dimB;
    } else {
        fprintf(stderr, "[YOLOv8n-det] ERROR: cannot identify feature dim "
                "(%u, %u) vs featureLen=%u\n", dimA, dimB, featureLen);
        return false;
    }

    for (unsigned i = 0; i < numDetections; ++i) {
        // Read cx, cy, w, h
        float cx, cy, bw, bh;
        if (!transposed) {
            // data layout: data[row * numDetections + col]
            cx = data[0 * numDetections + i];
            cy = data[1 * numDetections + i];
            bw = data[2 * numDetections + i];
            bh = data[3 * numDetections + i];
        } else {
            cx = data[i * featureLen + 0];
            cy = data[i * featureLen + 1];
            bw = data[i * featureLen + 2];
            bh = data[i * featureLen + 3];
        }

        // Find best class score — YOLOv8 exports post-sigmoid scores in [0,1]
        float maxScore = 0.0f;
        unsigned bestClassId = 0;
        for (unsigned c = 0; c < numClasses; ++c) {
            float score;
            if (!transposed) {
                score = data[(4 + c) * numDetections + i];
            } else {
                score = data[i * featureLen + 4 + c];
            }
            if (score > maxScore) {
                maxScore = score;
                bestClassId = c;
            }
        }

        // Per-class threshold
        float thr = (bestClassId < detectionParams.perClassPreclusterThreshold.size())
                     ? detectionParams.perClassPreclusterThreshold[bestClassId]
                     : 0.25f;
        if (maxScore < thr) continue;

        // Filter: only report person (class 0)
        // Accept both forklift (0) and person (1)

        // Convert centre-form to top-left corner (network-input pixel coords)
        float left   = cx - bw * 0.5f;
        float top    = cy - bh * 0.5f;
        float right  = cx + bw * 0.5f;
        float bottom = cy + bh * 0.5f;

        // Clamp to network input
        left   = std::max<float>(0.0f, std::min<float>(left,   netW));
        top    = std::max<float>(0.0f, std::min<float>(top,    netH));
        right  = std::max<float>(0.0f, std::min<float>(right,  netW));
        bottom = std::max<float>(0.0f, std::min<float>(bottom, netH));

        float w = right - left;
        float h = bottom - top;
        if (w <= 1.0f || h <= 1.0f) continue;

        NvDsInferObjectDetectionInfo obj;
        obj.classId = bestClassId;
        obj.left = left;
        obj.top = top;
        obj.width = w;
        obj.height = h;
        obj.detectionConfidence = maxScore;
        objectList.push_back(obj);
    }
    // ==== END GENERATED DECODE ====
    return true;
}